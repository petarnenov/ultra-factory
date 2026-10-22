package httpd

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/mobilebridge"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	"github.com/aoagents/agent-orchestrator/backend/internal/tailnet"
)

// authState keeps previously valid hashes so a client retrying an old password
// after rotation cannot lock out a newly paired client behind the same address.
type authState struct {
	hashes atomic.Pointer[authHashes]
	// tailnetOnly is set while the listener is bound to the Tailscale address;
	// the password then works only with passwordFallback, the explicit opt-in.
	tailnetOnly         atomic.Bool
	passwordFallback    atomic.Bool
	identityOnlyTailnet atomic.Bool
}

// passwordAllowed reports whether a connection password may authenticate a
// request on this listener.
func (a *authState) passwordAllowed() bool {
	return !a.tailnetOnly.Load() || a.passwordFallback.Load()
}

// TailnetAuthorizer vouches for a tailnet source address through the local
// tailscaled. tailnet.Authorizer is the production implementation.
type TailnetAuthorizer interface {
	Authorize(ctx context.Context, srcIP string) (tailnet.Identity, error)
}

// writeTailnetDenied answers a tailnet caller the authorizer rejected. The
// code tells the client whether to blame its identity or a missing tailscaled.
func writeTailnetDenied(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, tailnet.ErrForbidden) {
		envelope.WriteAPIError(w, r, http.StatusForbidden, "forbidden", "TAILNET_FORBIDDEN",
			"this host does not allow your Tailscale identity: "+err.Error(), nil)
		return
	}
	envelope.WriteAPIError(w, r, http.StatusForbidden, "forbidden", "TAILSCALE_UNAVAILABLE",
		"Tailscale is unavailable on this host, so remote requests are rejected: "+err.Error(), nil)
}

type authHashes struct {
	current string
	account string
	retired []string
}

func (a *authState) setHash(h string) {
	for {
		previous := a.hashes.Load()
		if previous != nil && previous.current == h {
			return
		}
		next := &authHashes{current: h}
		if previous != nil {
			next.account = previous.account
			next.retired = append(next.retired, previous.retired...)
			if previous.current != "" {
				next.retired = append(next.retired, previous.current)
			}
		}
		if a.hashes.CompareAndSwap(previous, next) {
			return
		}
	}
}
func (a *authState) setAccountHash(h string) {
	for {
		previous := a.hashes.Load()
		next := &authHashes{account: h}
		if previous != nil {
			next.current = previous.current
			next.retired = previous.retired
		}
		if a.hashes.CompareAndSwap(previous, next) {
			return
		}
	}
}
func (a *authState) accountHash() string {
	if hashes := a.hashes.Load(); hashes != nil {
		return hashes.account
	}
	return ""
}
func (a *authState) currentHash() string {
	if hashes := a.hashes.Load(); hashes != nil {
		return hashes.current
	}
	return ""
}

func (a *authState) retiredPasswordMatches(token string) bool {
	if hashes := a.hashes.Load(); hashes != nil {
		for _, hash := range hashes.retired {
			if mobilebridge.PasswordMatches(hash, token) {
				return true
			}
		}
	}
	return false
}

// maxAuthFailures is how many failed attempts lock a source key out until its
// window expires.
const maxAuthFailures = 5

// lockout throttles password guessing per source address.
type lockout struct {
	mu        sync.Mutex
	now       func() time.Time
	attempts  map[string]authAttempts
	lastSweep time.Time
}

type authAttempts struct {
	fails   int
	expires time.Time
}

func newLockout(now func() time.Time) *lockout {
	return &lockout{now: now, attempts: map[string]authAttempts{}}
}

func (l *lockout) blocked(src string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[src]
	if !ok {
		return false
	}
	if l.now().Before(a.expires) {
		return a.fails >= maxAuthFailures
	}
	delete(l.attempts, src)
	return false
}

// fail records a failed attempt and reports whether this attempt locked src
// out, so entering lockout is logged once rather than per failed request.
func (l *lockout) fail(src string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) >= time.Minute {
		for key, a := range l.attempts {
			if !now.Before(a.expires) {
				delete(l.attempts, key)
			}
		}
		l.lastSweep = now
	}
	a := l.attempts[src]
	if !now.Before(a.expires) {
		a.fails = 0
	}
	a.fails++
	a.expires = now.Add(time.Minute)
	l.attempts[src] = a
	return a.fails == maxAuthFailures
}

func (l *lockout) reset(src string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, src)
}

func (l *lockout) resetAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts = map[string]authAttempts{}
}

func sourceKey(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// Where a lockout key came from. Logged when a key becomes locked out.
const (
	lockoutKeyFromPeer           = "peer"
	lockoutKeyFromCFConnectingIP = "cf-connecting-ip"
	lockoutKeyFromForwardedFor   = "x-forwarded-for"
)

// lockoutKey derives the key failed attempts count against, starting from the
// TCP peer. It throttles guessing only, never authorization (ADR 0005).
func lockoutKey(r *http.Request, peer string, trustedProxy bool) (key, from string) {
	// Cloudflared connects from loopback, so use its visitor IP for lockout
	// only. A LAN client must not be able to spoof its source with this header.
	if ip := net.ParseIP(peer); ip != nil && ip.IsLoopback() {
		if forwarded := r.Header.Values("CF-Connecting-IP"); len(forwarded) == 1 {
			if visitor := net.ParseIP(forwarded[0]); visitor != nil {
				return visitor.String(), lockoutKeyFromCFConnectingIP
			}
		}
	}
	if trustedProxy {
		if client := rightmostForwardedFor(r.Header.Values("X-Forwarded-For")); client != nil {
			return client.String(), lockoutKeyFromForwardedFor
		}
	}
	return peer, lockoutKeyFromPeer
}

// rightmostForwardedFor returns the last X-Forwarded-For entry across all
// header lines, the one the nearest proxy appended; one trusted hop is assumed.
func rightmostForwardedFor(lines []string) net.IP {
	if len(lines) == 0 {
		return nil
	}
	entries := strings.Split(lines[len(lines)-1], ",")
	return net.ParseIP(strings.TrimSpace(entries[len(entries)-1]))
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// authCookieName carries the connection token for a preview page's in-page
// subresource requests. See connectionToken / maybeSetPreviewAuthCookie.
const authCookieName = "ao_conn"

// previewFilesMarker is the path segment that identifies a preview-file request
// (GET /api/v1/sessions/{id}/preview/files/*). The auth cookie is both scoped to
// and honored only on this path, so it can never authenticate any other endpoint.
const previewFilesMarker = "/preview/files/"

// previewFilesCookiePath returns the cookie Path to scope the auth cookie to the
// requesting session's preview files (".../preview/files/"), or "" if the request
// is not a preview-file request. Scoping this tightly is what keeps the cookie
// from ever reaching /kill, /send, another session, or any non-preview route.
func previewFilesCookiePath(urlPath string) string {
	i := strings.Index(urlPath, previewFilesMarker)
	if i < 0 {
		return ""
	}
	return urlPath[:i+len(previewFilesMarker)]
}

// connectionToken returns the caller's connection token. It comes from the
// Authorization: Bearer header (the mobile API client and a preview page's
// top-level navigation) or, ONLY on the preview-files route, the auth cookie (a
// preview page's subresource requests — images/CSS/JS — which the WebView issues
// without our header). Restricting the cookie to the preview-files path means it
// can never authenticate any other mobile endpoint even if a client sends it.
func connectionToken(r *http.Request) string {
	if t := bearerToken(r); t != "" {
		return t
	}
	if previewFilesCookiePath(r.URL.Path) != "" {
		if c, err := r.Cookie(authCookieName); err == nil {
			return c.Value
		}
	}
	return ""
}

// maybeSetPreviewAuthCookie drops the auth cookie when a preview FILE is fetched
// with a valid token, so the WebView's follow-up subresource requests on the same
// password-protected preview route authenticate too (they never carry our
// Authorization header). The cookie is Path-scoped to this session's preview
// files only, HttpOnly, and re-sent only when it doesn't already match the token
// that just authenticated — so a normal subresource costs no Set-Cookie, but a
// cookie left over from a regenerated password is overwritten instead of being
// kept until it 401s every image/CSS/JS on the page. This runs on the LAN
// listener only; the loopback/desktop preview path never reaches authMiddleware,
// so desktop preview behavior is unchanged.
func maybeSetPreviewAuthCookie(w http.ResponseWriter, r *http.Request, tok string) {
	path := previewFilesCookiePath(r.URL.Path)
	if path == "" {
		return
	}
	if c, err := r.Cookie(authCookieName); err == nil && c.Value == tok {
		return // already current; don't re-send Set-Cookie on every subresource
	}
	//nolint:gosec // Secure is intentionally omitted: the LAN bridge is plaintext
	// http by design (ADR 0001, home-network-only), and a Secure cookie would never
	// be sent over it. The token already travels the same plain link via Bearer.
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    tok,
		Path:     path,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// No Secure: the LAN link is plain http (a TLS tunnel still sends it),
		// matching how the Bearer token already travels.
	})
}

// authMiddleware authenticates LAN requests against the current connection
// password. connected, which may be nil, is notified of the source address of
// every request that authenticates; it exists so telemetry can observe that a
// phone actually reached this desktop, and it must not block the request, since
// it runs inline on every authenticated call.
// identityProbePath is the one route the LAN listener serves without the
// connection password. The phone races several endpoints, and a private
// address is not an identity: 192.168.1.42 exists on most networks. Verifying
// which machine answered has to happen BEFORE a credential is presented, or
// the phone leaks its token to whatever device holds that address on a foreign
// network. The response carries an opaque host id and nothing else.
//
// Exact path, GET only, and checked ahead of the lockout so a phone racing
// endpoints cannot lock itself out probing. See
// docs/adr/0003-unauthenticated-identity-probe.md.
const identityProbePath = "/api/v1/identity"

// isIdentityProbe reports whether r is the exempt probe. Deliberately an exact
// match rather than a prefix, so nothing nested below the path inherits the
// exemption.
func isIdentityProbe(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == identityProbePath
}

// tailnetAuth (may be nil) admits a tailnet caller on its Tailscale identity;
// a rejected caller may use the password only where passwordAllowed says so.

// trustedProxy is AO_REMOTE_TRUSTED_PROXY: when set, the lockout key comes from
// X-Forwarded-For. log, which may be nil, gets one line per new lockout.
func authMiddleware(state *authState, lock *lockout, connected *mobileConnectReporter, trustedProxy bool, tailnetAuth TailnetAuthorizer, log *slog.Logger) func(http.Handler) http.Handler {
	log = loggerOrDefault(log)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isIdentityProbe(r) {
				next.ServeHTTP(w, r)
				return
			}
			remoteSrc := sourceKey(r)
			src, srcFrom := lockoutKey(r, remoteSrc, trustedProxy)
			if lock.blocked(src) {
				envelope.WriteAPIError(w, r, http.StatusTooManyRequests, "too_many_requests", "LOCKED_OUT",
					"too many failed attempts; try again shortly", nil)
				return
			}
			if remotehost.IsTailnetHost(remoteSrc) && tailnetAuth == nil && state.identityOnlyTailnet.Load() {
				envelope.WriteAPIError(w, r, http.StatusForbidden, "forbidden", "TAILSCALE_UNAVAILABLE",
					"this listener requires a Tailscale identity check", nil)
				return
			}
			if tailnetAuth != nil && remotehost.IsTailnetHost(remoteSrc) {
				identity, err := tailnetAuth.Authorize(r.Context(), remoteSrc)
				if err == nil {
					lock.reset(src)
					connected.report(remoteSrc)
					next.ServeHTTP(w, r)
					return
				}
				if !state.passwordAllowed() || state.identityOnlyTailnet.Load() {
					log.Warn("remote-host auth: tailnet caller rejected", "peer", remoteSrc, "node", identity.NodeName, "err", err)
					writeTailnetDenied(w, r, err)
					return
				}
			} else if !state.passwordAllowed() {
				envelope.WriteAPIError(w, r, http.StatusForbidden, "forbidden", "TAILNET_REQUIRED",
					"this host accepts remote requests only from its Tailscale network", nil)
				return
			}
			tok := connectionToken(r)
			conn, _ := r.Context().Value(lanConnContextKey{}).(*lanConn)
			if conn != nil {
				conn.owner.mu.Lock()
			}
			// Record auth atomically with rotation's connection close.
			authenticated := mobilebridge.PasswordMatches(state.currentHash(), tok) ||
				mobilebridge.PasswordMatches(state.accountHash(), tok)
			if conn != nil {
				if authenticated {
					conn.tokenHash = mobilebridge.HashPassword(tok)
				}
				conn.owner.mu.Unlock()
			}
			if authenticated {
				lock.reset(src)
				connected.report(remoteSrc)
				maybeSetPreviewAuthCookie(w, r, tok)
				next.ServeHTTP(w, r)
				return
			}
			if !state.retiredPasswordMatches(tok) && lock.fail(src) {
				log.Warn("remote-host auth: source locked out after repeated failures",
					"key", src, "peer", remoteSrc, "key_from", srcFrom)
			}
			envelope.WriteAPIError(w, r, http.StatusUnauthorized, "unauthorized", "BAD_PASSWORD",
				"missing or invalid connection password", nil)
		})
	}
}

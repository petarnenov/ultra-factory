package remotehost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrCredentialRefused reports a 401 or 429 from a remote host: the saved
// password is wrong or the host is locking this machine out.
var ErrCredentialRefused = errors.New("remote host refused the saved password; update it in the desktop app under Settings → Remote hosts")

// Session is one identity-checked connection to a saved host. Open probes
// the identity before any credential is sent.
type Session struct {
	http     http.Client
	base     string
	hostID   string
	password string
}

// Open probes the saved host and returns a session bound to its paired
// identity. Only the probe may open a new connection; later requests reuse it.
func Open(ctx context.Context, base *http.Client, entry Entry, timeout time.Duration) (*Session, error) {
	if entry.HostID == "" {
		return nil, errors.New("saved host has no host id")
	}
	root, err := BaseURL(entry.URL)
	if err != nil {
		return nil, err
	}
	if err := UnattendedTransportAllowed(root, entry.AllowInsecureHTTP); err != nil {
		return nil, err
	}
	client, gate := ProbedClient(base, timeout)
	actual, err := ReadIdentity(ctx, client, root)
	gate.Close()
	if err != nil {
		closeIdle(client)
		return nil, err
	}
	if actual != entry.HostID {
		closeIdle(client)
		return nil, errors.New("remote host identity changed; re-pair it in the desktop app")
	}
	return &Session{http: client, base: root, hostID: entry.HostID, password: entry.Password}, nil
}

// Close releases the session's idle connections.
func (s *Session) Close() { closeIdle(s.http) }

// GetJSON performs an authenticated GET and decodes the JSON body into out.
// Errors never include the credential.
func (s *Session) GetJSON(ctx context.Context, path string, out any) error {
	// #nosec G704 -- identity-checked saved host; internal API route.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+path, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.password)
	req.Header.Set("X-AO-Expected-Host-ID", s.hostID)
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() {
		DrainBody(resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests {
		return ErrCredentialRefused
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GET %s: HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("GET %s: decode response: %w", path, err)
	}
	return nil
}

func closeIdle(client http.Client) {
	if transport, ok := client.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

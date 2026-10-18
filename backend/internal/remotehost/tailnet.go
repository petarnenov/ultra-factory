package remotehost

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TailnetSOCKSEnv names the loopback SOCKS5 listener the hub's Tailscale node
// exposes in userspace mode (ADR 0008); only tailnet destinations use it.
const TailnetSOCKSEnv = "AO_REMOTE_TAILNET_SOCKS"

// tailnetIPv6 is Tailscale's IPv6 range (ADR 0008, Decision item 3).
var tailnetIPv6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")

// IsTailnetHost reports whether host, as written, is a tailnet destination: an
// IP in 100.64.0.0/10 or fd7a:115c:a1e0::/48, or a .ts.net name; no DNS.
func IsTailnetHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if ip := net.ParseIP(host); ip != nil {
		addr, ok := netip.AddrFromSlice(ip)
		return tailscaleNet.Contains(ip) || (ok && tailnetIPv6.Contains(addr.Unmap()))
	}
	return strings.HasSuffix(host, ".ts.net")
}

// TailnetSOCKSAddress reads and validates TailnetSOCKSEnv: unset returns "".
// A set value must be host:port with a 127.0.0.0/8 or ::1 literal host.
func TailnetSOCKSAddress() (string, error) {
	raw := strings.TrimSpace(os.Getenv(TailnetSOCKSEnv))
	if raw == "" {
		return "", nil
	}
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return "", fmt.Errorf("%s must be a loopback host:port such as 127.0.0.1:1055: %w", TailnetSOCKSEnv, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("%s host must be an IP literal in 127.0.0.0/8 or ::1, got %q", TailnetSOCKSEnv, host)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("%s port must be 1-65535, got %q", TailnetSOCKSEnv, port)
	}
	return net.JoinHostPort(host, port), nil
}

// dialFunc matches http.Transport.DialContext.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// defaultDial is the dialer for transports that set none, matching
// http.DefaultTransport.
var defaultDial dialFunc = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext

// routeTailnet sends tailnet destinations through the SOCKS5 proxy named by
// TailnetSOCKSEnv, others to inner; a tailnet host never falls back (ADR 0008).
func routeTailnet(inner dialFunc) dialFunc {
	socks, err := TailnetSOCKSAddress()
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, splitErr := net.SplitHostPort(addr)
		if splitErr != nil || !IsTailnetHost(host) {
			return inner(ctx, network, addr)
		}
		if err != nil {
			return nil, err
		}
		if socks == "" {
			return inner(ctx, network, addr)
		}
		return socks5Connect(ctx, socks, host, port)
	}
}

// socks5Connect opens a CONNECT tunnel to host:port through the SOCKS5 proxy at
// proxy. Hostnames go unresolved, so MagicDNS names reach the proxy as written.
func socks5Connect(ctx context.Context, proxy, host, port string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", proxy)
	if err != nil {
		return nil, fmt.Errorf("tailnet SOCKS5 proxy %s: %w", proxy, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	err = socks5Handshake(conn, host, port)
	if !stop() && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("tailnet SOCKS5 connect to %s: %w", net.JoinHostPort(host, port), err)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// socks5Handshake runs the RFC 1928 no-auth greeting and CONNECT request.
func socks5Handshake(conn net.Conn, host, port string) error {
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		return err
	}
	var greeting [2]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil {
		return fmt.Errorf("greeting: %w", err)
	}
	if greeting[0] != 5 || greeting[1] != 0 {
		return errors.New("proxy refused the no-authentication method")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid port %q", port)
	}
	request := []byte{5, 1, 0} // VER, CMD=CONNECT, RSV
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			request = append(append(request, 1), v4...)
		} else {
			request = append(append(request, 4), ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return errors.New("host name longer than 255 bytes")
		}
		// len(host) is bounded above, so the conversion cannot truncate.
		// #nosec G115 -- length checked above.
		request = append(append(request, 3, byte(len(host))), host...)
	}
	request = binary.BigEndian.AppendUint16(request, uint16(n))
	if _, err := conn.Write(request); err != nil {
		return err
	}
	var head [4]byte // VER, REP, RSV, ATYP
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		return fmt.Errorf("reply: %w", err)
	}
	if head[0] != 5 {
		return errors.New("proxy did not speak SOCKS5")
	}
	if head[1] != 0 {
		return fmt.Errorf("proxy refused the connection (reply %d)", head[1])
	}
	var bound int
	switch head[3] {
	case 1:
		bound = 4
	case 4:
		bound = 16
	case 3:
		var length [1]byte
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			return fmt.Errorf("reply: %w", err)
		}
		bound = int(length[0])
	default:
		return fmt.Errorf("proxy reply has unknown address type %d", head[3])
	}
	// Discard the bound address and port so the tunnel starts clean.
	_, err = io.CopyN(io.Discard, conn, int64(bound+2))
	return err
}

// ErrUnprobedConnection is returned for a dial that the identity probe did not
// make. The bearer may only travel on a connection that answered the probe.
var ErrUnprobedConnection = errors.New("remote host connection was not made by the identity probe; the pass stops and retries on the next poll")

// ProbeGate allows exactly one connection through: the identity probe's. After
// that a bearer request only rides a connection that answered the probe.
type ProbeGate struct {
	mu        sync.Mutex
	dialsLeft int
}

// take consumes one dial allowance, and reports false once none is left.
func (g *ProbeGate) take() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.dialsLeft <= 0 {
		return false
	}
	g.dialsLeft--
	return true
}

// Close ends the gate, so no further connection can be dialed through it. Call
// it when the probe returns, whether or not the probe succeeded.
func (g *ProbeGate) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dialsLeft = 0
}

func (g *ProbeGate) guard(inner dialFunc) dialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !g.take() {
			return nil, ErrUnprobedConnection
		}
		return inner(ctx, network, addr)
	}
}

// ProbedClient is DirectClient with a ProbeGate on its transport, for a host's
// pass: ReadIdentity runs first, then the gate closes (ADR 0008, item 8).
func ProbedClient(base *http.Client, timeout time.Duration) (http.Client, *ProbeGate) {
	gate := &ProbeGate{dialsLeft: 1}
	return directClient(base, timeout, gate), gate
}

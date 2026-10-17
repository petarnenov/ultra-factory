package remotehost

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost/sockstest"
)

func TestIsTailnetHostCoversCGNATIPv6AndMagicDNSNames(t *testing.T) {
	for host, want := range map[string]bool{
		"100.64.0.1":             true,
		"100.127.255.254":        true,
		"100.128.0.1":            false,
		"100.63.255.255":         false,
		"fd7a:115c:a1e0::1":      true,
		"fd7a:115c:a1e0:ffff::9": true,
		"fd7a:115c:a1e1::1":      false,
		"::ffff:100.64.0.1":      true,
		"build.ts.net":           true,
		"BUILD.TS.NET.":          true,
		"build.ts.net..":         false,
		"ts.net":                 false,
		"ts.net.example.com":     false,
		"example.com":            false,
		"127.0.0.1":              false,
		"":                       false,
	} {
		if got := IsTailnetHost(host); got != want {
			t.Errorf("IsTailnetHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestTailnetSOCKSAddressAcceptsOnlyLoopbackHostPort(t *testing.T) {
	t.Setenv(TailnetSOCKSEnv, "")
	if addr, err := TailnetSOCKSAddress(); err != nil || addr != "" {
		t.Fatalf("unset = %q, %v", addr, err)
	}
	for raw, want := range map[string]string{
		"127.0.0.1:1055": "127.0.0.1:1055",
		"[::1]:1055":     "[::1]:1055",
		" 127.0.0.2:9 ":  "127.0.0.2:9",
	} {
		t.Setenv(TailnetSOCKSEnv, raw)
		if addr, err := TailnetSOCKSAddress(); err != nil || addr != want {
			t.Errorf("%q = %q, %v; want %q", raw, addr, err, want)
		}
	}
	for _, raw := range []string{"localhost:1055", "10.0.0.5:1055", "0.0.0.0:1055", "[::]:1055", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:70000", "127.0.0.1:x", "example.com:1055"} {
		t.Setenv(TailnetSOCKSEnv, raw)
		if addr, err := TailnetSOCKSAddress(); err == nil {
			t.Errorf("%q accepted as %q", raw, addr)
		}
	}
}

// startTarget serves an identity and an authenticated route on loopback.
func startTarget(t *testing.T, hostID string) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/identity" {
			_, _ = w.Write([]byte(`{"hostId":"` + hostID + `","apiVersion":1}` + "\n"))
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	return srv, strconv.Itoa(port)
}

func TestDirectClientRoutesTailnetDestinationsThroughSOCKS(t *testing.T) {
	_, port := startTarget(t, "host-build")
	proxy := sockstest.Start(t, func(string) string { return "127.0.0.1" })
	t.Setenv(TailnetSOCKSEnv, proxy.Addr)

	client := DirectClient(nil, 5*time.Second)
	base := "http://build.ts.net:" + port
	id, err := ReadIdentity(context.Background(), client, base)
	if err != nil || id != "host-build" {
		t.Fatalf("ReadIdentity = %q, %v", id, err)
	}
	if got := proxy.Targets(); len(got) != 1 || got[0] != "build.ts.net:"+port {
		t.Fatalf("SOCKS targets = %v, want the unresolved name", got)
	}
}

func TestDirectClientNeverSendsNonTailnetDestinationsToSOCKS(t *testing.T) {
	_, port := startTarget(t, "host-local")
	proxy := sockstest.Start(t, nil)
	t.Setenv(TailnetSOCKSEnv, proxy.Addr)

	client := DirectClient(nil, 5*time.Second)
	if _, err := ReadIdentity(context.Background(), client, "http://127.0.0.1:"+port); err != nil {
		t.Fatalf("direct ReadIdentity: %v", err)
	}
	if proxy.Connections() != 0 {
		t.Fatalf("non-tailnet destination touched SOCKS: %d connections", proxy.Connections())
	}
}

func TestDirectClientIgnoresEnvironmentProxies(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	t.Setenv("ALL_PROXY", "socks5://127.0.0.1:9")
	client := DirectClient(nil, time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatalf("transport proxy = %#v, want nil", client.Transport)
	}
	if client.CheckRedirect == nil {
		t.Fatal("redirect guard missing")
	}
}

func TestDirectClientRefusesToDialTailnetWithAnInvalidProxyAndNeverFallsBack(t *testing.T) {
	_, port := startTarget(t, "host-build")
	t.Setenv(TailnetSOCKSEnv, "localhost:1055")
	client := DirectClient(nil, 2*time.Second)
	_, err := ReadIdentity(context.Background(), client, "http://build.ts.net:"+port)
	if err == nil || !strings.Contains(err.Error(), TailnetSOCKSEnv) {
		t.Fatalf("err = %v, want the invalid-variable error and no direct dial", err)
	}
}

func TestProbedClientAllowsOnlyTheProbeConnection(t *testing.T) {
	// A "/close" response ends its connection, so the transport cannot keep
	// it idle; the next request has to dial, which the gate must refuse.
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/close" {
			w.Header().Set("Connection", "close")
		}
		_, _ = w.Write([]byte(`{"hostId":"host-build","apiVersion":1}`))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	client, gate := ProbedClient(nil, 5*time.Second)
	if _, err := ReadIdentity(context.Background(), client, srv.URL); err != nil {
		t.Fatalf("probe: %v", err)
	}
	gate.Close()

	// The probe's connection returns to the idle pool asynchronously after its
	// body is read, so a request can race it and be refused (never sent). Retry
	// until it is reused; no request may open a second connection.
	for _, path := range []string{"/api/v1/sessions", "/close"} {
		deadline := time.Now().Add(5 * time.Second)
		for {
			resp, err := client.Get(srv.URL + path)
			if err == nil {
				_ = resp.Body.Close()
				break
			}
			if !errors.Is(err, ErrUnprobedConnection) || time.Now().After(deadline) {
				t.Fatalf("GET %s on the probed connection: %v", path, err)
			}
			time.Sleep(time.Millisecond)
		}
	}
	if got := conns.Load(); got != 1 {
		t.Fatalf("connections opened = %d, want only the probe's", got)
	}

	// The "/close" response ended the probe's connection, so a new dial is refused.
	if _, err := client.Get(srv.URL + "/api/v1/sessions"); !errors.Is(err, ErrUnprobedConnection) {
		t.Fatalf("err = %v, want ErrUnprobedConnection", err)
	}
}

func TestProbedClientRefusesAnUnprobedFirstConnection(t *testing.T) {
	srv, _ := startTarget(t, "host-build")
	client, gate := ProbedClient(nil, 5*time.Second)
	gate.Close()
	if _, err := client.Get(srv.URL + "/anything"); !errors.Is(err, ErrUnprobedConnection) {
		t.Fatalf("err = %v, want ErrUnprobedConnection", err)
	}
}

func TestUnattendedTransportAllowedUsesTheTailnetPredicate(t *testing.T) {
	for base, wantOK := range map[string]bool{
		"http://[fd7a:115c:a1e0::5]:3011": true,
		"http://build.ts.net.:3011":       true,
		"http://100.100.1.1:3011":         true,
		"http://[2001:db8::1]:3011":       false,
		"http://example.com:3011":         false,
	} {
		err := UnattendedTransportAllowed(base)
		if (err == nil) != wantOK {
			t.Errorf("UnattendedTransportAllowed(%q) = %v, want ok=%v", base, err, wantOK)
		}
	}
}

func TestSOCKSHandshakeErrorsNameTheTarget(t *testing.T) {
	// Port 9 on loopback has no listener, so the proxy reports a refused connect.
	proxy := sockstest.Start(t, nil)
	_, err := socks5Connect(context.Background(), proxy.Addr, "build.ts.net", "9")
	if err == nil || !strings.Contains(err.Error(), "build.ts.net:9") {
		t.Fatalf("err = %v", err)
	}
}

// Package sockstest is a fake SOCKS5 proxy for tests of tailnet routing: it
// records every connection and CONNECT target, tunnelling to a loopback host.
package sockstest

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
)

// Proxy is a running fake SOCKS5 server.
type Proxy struct {
	// Addr is the loopback host:port to put in AO_REMOTE_TAILNET_SOCKS.
	Addr string

	mu      sync.Mutex
	conns   int
	targets []string
}

// Start serves SOCKS5 on a loopback port until t ends. resolve maps a requested
// host name to the loopback host that receives the tunnel; nil keeps the name.
func Start(t *testing.T, resolve func(host string) string) *Proxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{Addr: ln.Addr().String()}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.conns++
			p.mu.Unlock()
			go p.serve(conn, resolve)
		}
	}()
	return p
}

// Connections returns how many TCP connections the proxy accepted.
func (p *Proxy) Connections() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conns
}

// Targets returns the host:port of every CONNECT request, in order.
func (p *Proxy) Targets() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.targets...)
}

func (p *Proxy) serve(client net.Conn, resolve func(string) string) {
	defer func() { _ = client.Close() }()
	var head [2]byte
	if _, err := io.ReadFull(client, head[:]); err != nil || head[0] != 5 {
		return
	}
	if _, err := io.CopyN(io.Discard, client, int64(head[1])); err != nil {
		return
	}
	if _, err := client.Write([]byte{5, 0}); err != nil {
		return
	}
	var req [4]byte // VER, CMD, RSV, ATYP
	if _, err := io.ReadFull(client, req[:]); err != nil || req[0] != 5 || req[1] != 1 {
		return
	}
	var host string
	switch req[3] {
	case 1:
		var ip [4]byte
		if _, err := io.ReadFull(client, ip[:]); err != nil {
			return
		}
		host = net.IP(ip[:]).String()
	case 4:
		var ip [16]byte
		if _, err := io.ReadFull(client, ip[:]); err != nil {
			return
		}
		host = net.IP(ip[:]).String()
	case 3:
		var length [1]byte
		if _, err := io.ReadFull(client, length[:]); err != nil {
			return
		}
		name := make([]byte, length[0])
		if _, err := io.ReadFull(client, name); err != nil {
			return
		}
		host = string(name)
	default:
		return
	}
	var portBytes [2]byte
	if _, err := io.ReadFull(client, portBytes[:]); err != nil {
		return
	}
	port := strconv.Itoa(int(binary.BigEndian.Uint16(portBytes[:])))
	p.mu.Lock()
	p.targets = append(p.targets, net.JoinHostPort(host, port))
	p.mu.Unlock()

	target := host
	if resolve != nil {
		target = resolve(host)
	}
	upstream, err := net.Dial("tcp", net.JoinHostPort(target, port))
	if err != nil {
		// connection refused
		_, _ = client.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer func() { _ = upstream.Close() }()
	if _, err := client.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, upstream); done <- struct{}{} }()
	<-done
}

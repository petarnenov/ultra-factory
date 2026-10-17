# RW-1.3.2 · Task: Dual-stack bind of the remote-host listener
**Status:** ✅ done (#66)

**Type:** backend Go. **Estimate:** 0.5–1 pd. **Story:** RW-1.3. **Dependencies:** RW-1.1.3.

## Goal
The listener can listen on `[::]` (IPv4+IPv6) so that it is reachable over Railway private networking.

## Context
- `backend/internal/httpd/lan_listener.go:201-204` `Start` → `m.start(port, "0.0.0.0")`.
- `backend/internal/mobilebridge/netiface.go` lists the addresses shown during pairing.
- The decision from RW-1.1.3 determines whether this is opt-in (`AO_REMOTE_BIND=dual`) or the default.

## Subtasks
- [ ] Config: `AO_REMOTE_BIND` ∈ {`ipv4` (default), `dual`}; validation and a test in `config_test.go`.
- [ ] `LANManager.Start` picks host `"0.0.0.0"` or `"::"` according to the configuration. `StartLoopback` stays on `127.0.0.1`.
- [ ] Make sure `net.JoinHostPort` works with IPv6 (square brackets) everywhere the address is printed (`remote_host.go:84`, status responses).
- [ ] `netiface.go`: in dual mode, also include the global IPv6 addresses in the list, or keep only IPv4 for the pairing UI, per the ADR.
- [ ] Test: a listener on `::` accepts connections on both `127.0.0.1` and `::1`.

## Acceptance criteria
- [ ] `AO_REMOTE_BIND=dual ao daemon` + enable → `curl http://127.0.0.1:3011/api/v1/identity` and `curl 'http://[::1]:3011/api/v1/identity'` → 200.
- [ ] Without the env var → behavior as today.

# RW-1.1.3 · Task: ADR for a dual-stack bind of the remote-host listener

**Type:** ADR. **Estimate:** 0.5 pd. **Story:** RW-1.1.

## Goal
Decide whether `LANManager.Start` should bind `[::]` (IPv4+IPv6) instead of `0.0.0.0`, so that it is reachable over IPv6 private networking.

## Context
- `backend/internal/httpd/lan_listener.go:202` — `m.start(port, "0.0.0.0")`.
- Railway private networking is (at least historically) IPv6-only; `*.railway.internal` resolves to IPv6. Verifying this is part of RW-1.4.
- Go `net.Listen("tcp", "[::]:3011")` on Linux is dual-stack by default (`net.ipv6.bindv6only=0`). Same on macOS. Behavior for LAN users does not change, except that the listener also becomes reachable over IPv6 on the LAN.
- ADR `0001` describes the LAN listener. Changing its bind address is within its scope.

## Subtasks
- [ ] Check the behavior of `bindv6only` in a typical Railway container (Debian/trixie).
- [ ] Decide: always dual-stack or opt-in env `AO_REMOTE_BIND=dual`. Recommendation: opt-in for the first release, so LAN users' behavior does not change without warning.
- [ ] Note the effect on `netiface.go` (the list of addresses the UI shows for pairing).
- [ ] Write an ADR or an addendum to `0001`.

## Acceptance criteria
- [ ] The decision is recorded and RW-1.3.2 has a clear specification.

# RW-1.1.1 · Task: ADR for trusting a reverse proxy in front of the remote-host listener

**Type:** ADR. **Estimate:** 0.5 pd. **Story:** RW-1.1.

## Goal
Decide whether and how `authMiddleware` should take the client IP from a proxy header when the daemon runs behind the Railway edge (a TLS-terminating proxy).

## Context
- `backend/internal/httpd/auth.go:105` — lockout after 5 failures/min on `sourceKey` = `RemoteAddr`.
- `auth.go:260-270` — `CF-Connecting-IP` is used only when the peer is loopback (cloudflared).
- `X-Forwarded-For` is not read anywhere. Behind Railway all clients appear with a single IP, and one attacker locks everyone out for a minute.
- Precedent: `docs/adr/0004-*` (Cloudflare tunnel).

## Subtasks
- [ ] Research which header the Railway edge sends (`X-Forwarded-For`, `X-Real-IP`) and whether the proxy's peer IP is predictable.
- [ ] Choose a model: (a) opt-in env `AO_REMOTE_TRUSTED_PROXY=1` → use the first `X-Forwarded-For`; (b) alternative: lockout per credential instead of per IP. Recommendation: (a) + keep the per-IP lockout, never on by default.
- [ ] Describe why the header cannot be spoofed (the client does not reach the socket directly; private networking is a separate case).
- [ ] Write `docs/adr/000N-trusted-proxy-remote-host.md`: Context, Decision, Consequences, Security notes.
- [ ] Add a reference in the `AGENTS.md` hard rules.

## Acceptance criteria
- [ ] The ADR answers: when the flag is safe, when it is dangerous (LAN mode without a proxy), what gets logged.
- [ ] Reviewed by a second person.

## Verification
No code. Review of the document.

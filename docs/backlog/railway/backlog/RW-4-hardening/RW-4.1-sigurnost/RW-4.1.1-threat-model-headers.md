# RW-4.1.1 · Task: Threat model and security headers
**Status:** ✅ done (#98)

**Type:** security + Node TS. **Estimate:** 0.5–1 pd. **Story:** RW-4.1.

## Subtasks
- [ ] `docs/railway/threat-model.md`: assets (Railway token, host credentials, agent keys, the code in worktrees), attackers, vectors, mitigations, residual risk. Explicitly: "access to the UI = a shell on all workers".
- [ ] BFF headers: `Strict-Transport-Security`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Permissions-Policy`, `Content-Security-Policy` aligned with RW-2.1.2 (the BFF serves the SPA, so the CSP can be a header, not a meta tag).
- [ ] Session: invalidate on operator password change; `GET /hub/sessions` + `DELETE` for logout everywhere.
- [ ] Verify that a 401 from a worker does not leak hostId/URL to an unauthenticated client.
- [ ] Tests for the headers.

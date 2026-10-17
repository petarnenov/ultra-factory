# RW-1.4.1 · Task: Deploy to Railway and networking checks
**Status:** ⛔ needs-human

**Type:** operations. **Estimate:** 0.5 pd. **Story:** RW-1.4.

## Subtasks
- [ ] Railway project `ao-lab`; service `worker-1` from image `ghcr.io/<org>/ao-worker:<tag>`; volume mounted at `/data`; variables: `AO_REMOTE_PASSWORD`, `AO_REMOTE_BIND=dual`, `AO_REMOTE_TRUSTED_PROXY=1`, `ANTHROPIC_API_KEY` (or an OAuth token), `GH_TOKEN`, `AO_GIT_USER_*`; target port 3011; public domain.
- [ ] Public URL: `curl https://<domain>/api/v1/identity` → 200; wrong password ×5 from one client → 429/lockout for that client only (with a second client via a different IP, or by checking the logs).
- [ ] Private networking: a second temporary service (`curlimages/curl`) → `curl http://worker-1.railway.internal:3011/api/v1/identity`. Record whether it resolves to IPv6 and whether it responds. If the IPv4-only bind does not work, that confirms the need for RW-1.3.2.
- [ ] `railway redeploy` → check that the hostId and the password are unchanged and the DB is preserved.
- [ ] Measure: time from deploy to `/readyz`, image size in the Railway UI, memory at idle.
- [ ] Record the results in `deploy/railway/README.md`, section "Validation log <date>".

## Acceptance criteria
- [ ] Every check above has a recorded result (pass/fail + note).

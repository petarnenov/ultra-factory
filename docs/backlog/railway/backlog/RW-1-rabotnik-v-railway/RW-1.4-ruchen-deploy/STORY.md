# RW-1.4 · Story: Manual worker deploy and connection from the desktop app

**As** an operator **I want** to deploy a single worker to Railway following a documented procedure and use it from the desktop app, **so that** we confirm networking, the image, and agent authentication before RW-2.

**Estimate:** 1 pd (+ time waiting on Railway). **Dependencies:** RW-1.2, RW-1.3. **Blocks:** RW-2.4, RW-3.

| Task | Name | pd |
|---|---|---|
| RW-1.4.1 | Deploy to Railway and networking checks | 0.5 |
| RW-1.4.2 | End-to-end session from the desktop app and runbook | 0.5 |

**Acceptance criteria**
- [ ] Recorded results: reachability of :3011 via the public domain and via `*.railway.internal` (IPv4/IPv6), behavior on redeploy, image size, startup time.
- [ ] `docs/self-hosted-remote.md` or a new `deploy/railway/README.md` describes the procedure step by step.

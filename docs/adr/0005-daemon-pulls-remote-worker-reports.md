# 5. The daemon pulls remote workers' reports from saved hosts

Date: 2026-10-16
Status: Accepted

## Context

`ao spawn --host <saved host>` starts a worker on another machine's daemon.
Session IDs are per daemon, so the CLI drops the parent link for a remote
spawn. The worker's `ao report` then lands in its own host's report outbox.
`report.Coordinator` delivers only to an active orchestrator in the same
daemon's store, and a worker-only host has none. The report stays `pending`
for good, and the orchestrator that spawned the worker is never told. Issue #77
has the details. Until now, the RW backlog polled the hosts with a launchd
ticker instead.

The remote daemon cannot push to the orchestrator's machine. That daemon's
primary listener is loopback only, and AGENTS.md forbids any other bind. Its
optional LAN listener is for phones and is often off.

## Decision

The orchestrator's daemon pulls from the worker's daemon. The only traffic is
outbound HTTP to hosts the user has already paired in the desktop app.

1. **Link.** After a successful `ao spawn --host` inside an AO session, the CLI
   calls `POST /api/v1/remote-worker-links` on its own daemon with the
   parent session, host ID, remote session, and remote project. The daemon
   checks three things: the parent exists and is not terminated, the host is a
   saved host other than this machine, and the host's label comes from
   `remotes.json` and not from the caller. It then stores the link against the
   parent's project. The route is blocked on the LAN listener, so a phone
   cannot make the daemon contact hosts.
2. **Pull.** `internal/observe/remotereports` polls every linked host (every 15
   s, and right after a new link). Per host it resolves the saved entry, checks
   the transport policy below, then runs the unauthenticated identity probe and
   requires the answer to equal the saved host ID before it sends the saved
   credential. The probe, the pulls, and the acknowledgements of one pass share
   one client that stays open until the pass has settled, so the credential
   goes over the connection that answered the probe whenever the host keeps it
   open. It lists the worker's due
   reports with `GET /api/v1/reports/pending?sessionId=` (at most 200 reports and
   2 MiB of encoded JSON per pull, never reordered, while report creation is capped at 256 KiB per request;
   `morePending` says more are waiting or still inside their batching or
   done-settlement delay, judged by the remote clock, and a terminated worker's
   queue is due at once). Reports that break the
   content contract or the size limits (ID 128 bytes, output reference 2048 and
   label 200 characters) are acknowledged without delivery so they cannot pin
   the link. A 401 or 429 stops all requests to that host until its saved
   password changes, so a rotated password cannot trip the host's lockout.
   Each host gets a 45 s budget for its pulls and, from its first
   acknowledgement, another 45 s for its acknowledgements across every project
   of the pass. Once a host runs out of either budget, or an acknowledgement to
   it fails, all its remaining acknowledgements in that pass wait for the next
   pass; the local delivery record keeps them from being delivered twice.
3. **Deliver.** `Coordinator.DeliverExternal` submits each project's new
   reports to that project's active orchestrator in one turn, so a respawned
   orchestrator receives them as well. The reports are attributed as
   `name (session S on host H)` rather than with an `ao://` link, which would
   name a local session that does not exist.
4. **Acknowledge.** After a delivery whose prompt write succeeded - a nil
   return, or the acceptance timeout that follows a written prompt - the
   observer records the remote report IDs in
   `remote_worker_report_deliveries`. A timeout means the write landed, not
   that the orchestrator received the prompt: it can still sit unsubmitted.
   The observer then calls
   `POST /api/v1/reports/acknowledge` on the remote host. That call moves only
   that worker's still-pending reports to `acknowledged` with a `pulled:`
   token, and leaves alone any report the remote daemon already claimed for an
   orchestrator of its own. If the acknowledgement fails, the next pass
   acknowledges the same reports again but does not deliver them twice. After
   any other delivery error - no active orchestrator, a refused target, or a
   wait that was cancelled once the write succeeded, such as `ctx.Err()` -
   the observer acknowledges nothing and the reports stay pending on the
   remote host, so the next pass writes the prompt again. That is
   at-least-once and can duplicate a prompt that was already written.
5. **Retire.** When a worker has nothing pending at all (due or not) and the
   remote daemon reports its session terminated, or it answers
   `SESSION_NOT_FOUND`, the observer deletes the link, and the delivery history
   goes with it. A done report still settling keeps the link alive until it is
   delivered, and a terminated worker's pending list is read once more before
   the link is dropped, in case it reported while its session was being read. Any other 404, such as an older AO without these routes, keeps
   the link.

### Discovery

Spawning from an AO session is not the only way a worker reaches a saved
host: a worker started by hand on the host, or by an orchestrator that no
longer runs on this daemon, has no link. The daemon therefore also discovers
links itself. A discovery pass runs on daemon start, then every 60 s
(`AO_REMOTE_DISCOVERY_INTERVAL` overrides the interval for tests), and at once
when `remotes.json` changes, including a re-pasted password. `POST
/api/v1/remote-worker-links/sync` runs one pass on demand and answers with
each host's linked and skipped counts, plus why a host was blocked or failed.

Per saved host, the pass uses the same transport policy, identity probe, and
refusal memory as the pull. A host that is blocked or refused is not
contacted; a refused password is never sent again until the saved entry
changes. The pass lists the host's active sessions with
`GET /api/v1/sessions?active=true`, and links each `kind == worker`
session that is not terminated and whose project this daemon has. A link is
written with `source = discovered` and root owner, and only if no link exists:
an existing link keeps its owner and source. The 200-link cap applies; when it
is reached the pass stops linking and logs one line per host per pass. A
linked worker's already pending reports reach the orchestrator on the same
pass, through the normal pull. Terminated and vanished workers are never
linked, so discovery does not undo the retirement in step 5.

Each host has its own budget, and the hosts are scanned in parallel, so an
unreachable host does not delay the others.

### Transport policy

The daemon sends a saved password with no human acting, on whatever network the
machine is on, every pass. The identity probe proves nothing against an
eavesdropper: the host ID is public. Unattended pulling therefore runs only over
https, loopback (an SSH tunnel), or Tailscale (100.64.0.0/10 or `*.ts.net`).
Plain http to any other address needs `AO_REMOTE_REPORTS_ALLOW_HTTP=1` in the
daemon's environment (the daemon adopts the user's login-shell environment), a
deliberate choice for a trusted network. A blocked host's link is still stored,
and `ao spawn --host` prints why its reports will not arrive.

Links are capped at 200, and validated: remote session and project IDs must be
AO-shaped IDs (`[A-Za-z0-9][A-Za-z0-9._-]{0,127}`), display names lose control
characters, because all of them reach the orchestrator's prompt.

## Consequences

- `GET /api/v1/reports/pending` and `POST /api/v1/reports/acknowledge` are on the
  app API: unauthenticated on loopback like every app route, and behind
  `authMiddleware` on the LAN listener, tunnel, or proxy that `--host` already
  uses. `POST /api/v1/remote-worker-links` is loopback only.
- The orchestrator's daemon reads `~/.ao/remotes.json`, the same file and
  permission check as the CLI, through the shared `internal/remotehost`
  package.
- Delivery is at least once in two narrow cases. First, the daemon crashes
  after a submission was accepted but before it was recorded and
  acknowledged. Second, a new report becomes due before the next pass, which
  changes the batch's idempotency key. In either case the orchestrator can see
  a report twice. If recording fails but the acknowledgement succeeds, nothing
  is redelivered.
- Remote `stuck` reports are submitted but do not interrupt a busy
  orchestrator. Local `stuck` reports still do.
- If the remote project also has an active orchestrator, a report can reach
  both daemons: the acknowledgement leaves reports alone once the remote
  daemon has claimed them, but the pull may already have read them. AO does not
  support that setup otherwise either.
- Only reports cross hosts. Changes to a remote worker's PR or CI do not wake
  the local orchestrator directly: the remote daemon's SCM observer already
  nudges the worker, and the worker tells its orchestrator through
  `ao report`, which this flow carries. Mirroring remote SCM events would need
  a second pulled feed and is out of scope.
- Any authenticated caller can acknowledge a worker's pending reports. Such a
  caller can already create, kill, and message sessions.
- Longer term, a challenge-response (an HMAC of a server nonce) would remove the
  need to send the bearer at all; it would change the pairing contract and is
  out of scope here.

## Amendment: per-puller acknowledgement (2026-10-21, issue #455)

Two daemons on one machine can pull the same worker, for example the
production daemon and the dev daemon. Under the original rule the first
acknowledgement settled a report for every puller, so the other daemon's
orchestrator never received it. This amendment keeps reports pending per puller.

1. **Puller identity.** A daemon's puller ID is its own instance ID from
   `GET /api/v1/identity` (`hostId`, persisted per data dir). It is sent as
   `pullerId` on `GET /api/v1/reports/pending` and as `pullerId` in the body of
   `POST /api/v1/reports/acknowledge`.
2. **Per-puller acknowledgement.** An acknowledgement with a puller ID records
   `(report, puller)` in `report_pull_acks`. It does not change the report's
   `delivery_state`. The report stays due for every other puller. It leaves
   this daemon's own local schedule and claims, so a backlog of reports that
   only peers deliver cannot fill the scan window. A pull by puller P omits
   the reports P acknowledged.
3. **Legacy semantics.** A pull or acknowledgement without a puller ID keeps the
   original behaviour: the acknowledgement moves the report to `acknowledged`
   for everyone.
4. **Capability.** A host that honours a puller ID answers `pullerAcks: true` on
   the pending response. A puller that sent a puller ID but got no `pullerAcks`
   is talking to an older host. It logs one warning per host and acknowledges
   without a puller ID, which is first-delivery semantics. It never sends
   `pullerId` to such a host, because those hosts reject unknown body fields.
5. **Retention.** Pending reports of a worker expire when they are older than 7
   days or beyond the newest 10 000 reports of that session, whichever comes
   first. Expiry moves a report to `acknowledged` with claim token `expired`,
   so the host's own coordinator does not deliver it either. Before the report
   moves, each known puller that has not acknowledged it gains one in
   `report_pullers.expired_total`. Expiry runs on pulls for that worker.
6. **Expired counter.** `expired_total` only grows. A pending response carries
   `expiredTotal` and `expiredBeforePull`, the total minus the `expiredSeen`
   the puller sent. The puller logs the unseen count, then sends the total it
   logged as `expiredSeen` on its next pull. Nothing is subtracted on the host,
   so a pull repeated with the same `expiredSeen`, after a lost response or a
   failure later in the pull, returns the same count. A puller reads no count
   for reports it never pulled before.

Consequences: each puller gets every report once, unless it misses the
retention window. Once a peer has acknowledged a report, this daemon's own
orchestrator no longer receives it, so a worker host's local orchestrator only
sees reports that no peer has pulled yet.
Mixing legacy pullers with per-puller pullers on one worker gives legacy
semantics for the reports a legacy puller acknowledges.

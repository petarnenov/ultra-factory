# Per-process resource baseline (CO-1.1.1, issue #218)

This document defines a repeatable, read-only capture for hub, worker and
Railway service resource use. It exists because the backlog records a 23.84 GB
service-level memory maximum against a 24 GB limit but no committed sample links
that figure to a process.

**Status: protocol drafted, not run.** No baseline exists yet. The 23.84 GB
figure is **unverified** and must not be attributed to the daemon, the BFF, the
orchestrator, a worker, or a harness until the capture below is complete and the
comparison template is filled from real samples. Running this protocol requires
a human with Railway workspace access and a six-worker workload; it does not
require a deployment or a configuration change.

## 1. What is being measured

| Target | Process-level source | Notes |
| --- | --- | --- |
| Hub daemon | `GET /api/v1/usage/sessions/memory` on the hub's loopback listener (`127.0.0.1:3001`) | Returns per-session RSS, process count, CPU percent, sample time, the per-process tree, host `system` memory, and `app` memory (daemon plus sessions plus reviewer panes). |
| Hub BFF | `ps` inside the hub container | The BFF is a separate `node .../hub/bff/src/main.ts` process; it is **not** part of the daemon's `app` reading. |
| Hub orchestrator | The same usage route, read with the session list | The orchestrator is a session, so it appears in `sessions[]` (use `--all` with `ao session top`, or read the route without a project filter). Its harness children are in `sessions[].processes`. |
| Worker sessions on `Linux`, `IMac`, `Z600` | That host's own daemon route (`http://127.0.0.1:<port>/api/v1/usage/sessions/memory`), or `ao session top --host <host>` from the hub | The hub daemon has no process-level view of a remote worker. `--host` reads the worker host's daemon over its authenticated listener, but `ao session top` prints only RSS and process counts; CPU and the per-process tree still require the host's own route, so run the sampler there. |
| Railway service CPU and memory | `railway usage projects --project <project>` and each service's **Metrics** tab | Service-level readings include container and platform overhead; they are not process readings. |

`ao session top --all --json` remains a useful cross-check, but its output is not
the route payload: entries live in `data[]` and embed the CLI session DTO (`id`,
not `sessionId`, plus `projectId`, `kind`, `harness`, `status`, `activity`, and
so on) with `rssBytes` and `processCount` added. Both memory fields are `null`
when a session's tree could not be measured, and `meta.totalRssBytes` sums only
the readings that exist. The output carries no memory-sample timestamp, so pair
it with `date -u` when capturing. It also omits CPU percent and the per-process
list, so it cannot satisfy the per-process part of this protocol on its own.

## 2. Workload definition

Choose one workload variant and record it in the comparison template. Both use
the same observation window and the same six tasks across the repeat run.

### Variant A: paired worker hosts (default)

This matches the agreed topology in `docs/backlog/railway/orchestrator-prompt.md`
and `AGENTS.md`: the hub coordinates only, and implementation work runs on the
three paired machines.

| Machine (`ao host ls` label) | Workers | Harnesses |
| --- | --- | --- |
| `Linux` | 2 | one `claude-code`, one `codex` |
| `IMac` | 2 | one `claude-code`, one `codex` |
| `Z600` | 2 | one `claude-code`, one `codex` |

- Six concurrent implementation tasks, one per worker. Use the same six issue
  numbers, branches and prompts for the capture and the repeat so the two
  windows are comparable. Each task must run at least one build and one test
  command so the sample covers the harness plus its build/test child tree, where
  the additional processes appear.
- The hub runs one orchestrator session.
- No implementation worker runs on the hub. A session started there must be
  stopped and moved before the window starts.

### Variant B: Railway worker services (alternative)

Six services named `ao-worker-<id>`, each running one session with the same
harness mix. This variant costs six always-on worker services and needs
`AO_HUB_MAX_WORKERS` above its default of `5` so the BFF registers the sixth
worker. That is a configuration change and conflicts with acceptance criterion 3
("no production deployment or configuration change"). Prefer Variant A unless
the operator explicitly accepts the change.

### Observation window

- **Primary window: 60 minutes of steady state, sampled every 60 seconds.**
  Start the window only after all six sessions report `working` and each has
  completed at least one build or test invocation. Record the UTC start and end
  of the window and the UTC start and end of each sampler.
- **Repeat the window once** with the same six tasks, hosts and harnesses.
  Report material variance (see the repeat-variance section of the comparison
  template in §5). The repeat is part of one run of this protocol.
- If the result must be compared directly with the backlog's 24-hour claim,
  extend the window to 24 hours at the same 60-second interval and say so in the
  template. Two 24-hour windows are usually not practical; the 60-minute window
  is the default because the acceptance criteria require a repeat.

## 3. Preflight (read-only)

Run from the machine holding the repositories and Railway CLI login. Record the
outputs in the run metadata; do not paste credentials.

```sh
date -u +%FT%TZ
ao --version
railway --version
railway whoami
railway status
ao host ls
ao project ls --host Linux
ao project ls --host IMac
ao project ls --host Z600
ao orchestrator ls --host Linux
ao orchestrator ls --host IMac
ao orchestrator ls --host Z600
```

- All three hosts must be `online`. If a host is offline, stop and report it;
  do not substitute a fourth host without recording the deviation.
- A host with an active local orchestrator refuses a worker from the hub
  (`LOCAL_ORCHESTRATOR_ACTIVE`). Stop that orchestrator or choose another
  machine before the window.
- Confirm the current worker cap in the hub service's **Variables** tab:
  record `AO_HUB_MAX_WORKERS` (default `5`). Do not edit it.
- Record the workspace compute limits:

```sh
railway usage limit status --target workspace
```

Open the hub service in Railway, then **Settings → Deploy → Replica Limits**,
and record the memory limit in force. Do not set or change it. The 24 GB figure
in the backlog is the value to compare against only if the live limit is still
24 GB; if it differs, record the live value and say so in the report.

Create a local directory outside the repository for raw samples and keep it in
`RUN_DIR` for every command below:

```sh
RUN_DIR="$HOME/ao-baseline-$(date -u +%Y%m%d)"
mkdir -p "$RUN_DIR"
```

Do not commit raw samples, deployment logs, credentials or host identities.

## 4. Capture protocol

All times are UTC. The sampler aligns every host to the same epoch boundaries
(for a 60-second interval, at second `00` of each minute), so start the three
worker samplers and the hub sampler within the same minute. If a host clock is
more than five seconds off, record the skew; do not change the clock as part of
this capture.

### 4.1 Hub daemon, BFF, orchestrator

Run the sampler inside the hub container through `railway ssh`. The command
copies `scripts/railway/resource-sample.mjs` from this checkout over stdin,
starts it for the window, and writes JSONL to `/tmp` inside the container:

```sh
railway ssh --project <project> --environment <environment> \
  --service <hub-service> -- \
  sh -c 'mkdir -p /tmp/ao-baseline && cat > /tmp/ao-baseline/resource-sample.mjs && node /tmp/ao-baseline/resource-sample.mjs --label hub --base-url http://127.0.0.1:3001 --interval 60 --duration 3600 --ps "ao-hub-supervisor|bff/src/main.ts|tailscaled|ao daemon" --out /tmp/ao-baseline/hub.jsonl' \
  < scripts/railway/resource-sample.mjs
```

If your `railway ssh` does not forward stdin, base64-encode the script locally
(`base64 < scripts/railway/resource-sample.mjs`), paste the single line into the
container as `echo '<base64>' | base64 -d > /tmp/ao-baseline/resource-sample.mjs`,
and run the same `node` command. Record the deviation.

After the window, copy the file out and check that it has a header, one sample
per minute and a footer:

```sh
railway ssh --project <project> --environment <environment> \
  --service <hub-service> -- cat /tmp/ao-baseline/hub.jsonl \
  > "$RUN_DIR/hub.jsonl"
wc -l "$RUN_DIR/hub.jsonl"
```

The hub route covers the daemon, every live session (including the
orchestrator) and reviewer panes. The `ps` patterns add the BFF, the supervisor,
and `tailscaled` where enabled. The sampler drops its own PID and
its parent shell PID before applying those filters, so the pattern text in its
own command line cannot appear as a sample. The route's `app.own` excludes
session trees; do not add `app.own` and `sessions[]` together without first
confirming that the session roots are not also descendants of the daemon. The
route already deduplicates those roots in `app.rssBytes`; treat `app.rssBytes`
as AO's total and `own` as the non-session remainder.

### 4.2 Each worker host

On each of `Linux`, `IMac` and `Z600`, from a checkout of this repository, set
the same `RUN_DIR` path and create it on that host:

```sh
cd <checkout>
RUN_DIR="$HOME/ao-baseline-$(date -u +%Y%m%d)"
mkdir -p "$RUN_DIR"
ao status   # record the "port:" line; the default is 3001
node scripts/railway/resource-sample.mjs \
  --label z600 \
  --base-url "http://127.0.0.1:<port>" \
  --interval 60 --duration 3600 \
  --ps 'claude|codex|opencode|tmux' \
  --out "$RUN_DIR/z600.jsonl"
```

Repeat with `--label linux` and `--label imac`, and keep the same paths on each
host. Running the sampler on the host avoids remote authentication: the primary
listener is loopback-only and unauthenticated by design. If you cannot work
directly on a host, run the same command over your usual host access and copy
the JSONL back; do not expose the primary listener to do it.

The route's `sessions[].processes` is the per-session process tree (PID, PPID,
RSS, CPU, command). The `ps` patterns catch harness or tmux processes that
detached from that tree. If a harness spawns a daemonised child, the route may
not see it; list it as unmeasured unless `ps` caught it.

### 4.3 Railway service samples

At the start and again at the end of each window, capture the project usage
summary and the limit status:

```sh
railway usage projects --project <project> \
  | tee "$RUN_DIR/railway-usage-start.txt"
railway usage limit status --target workspace \
  | tee "$RUN_DIR/railway-limits-start.txt"

# after the window
railway usage projects --project <project> \
  | tee "$RUN_DIR/railway-usage-end.txt"
railway usage limit status --target workspace \
  | tee "$RUN_DIR/railway-limits-end.txt"
```

`railway usage projects` is a period summary per service, not a time series.
Treat the two reads as window brackets, not as samples.

For each service in the window (the hub and every worker service, or the six
paired-host equivalents where Railway hosts them), open the service's
**Metrics** tab, set the range to the window, and record CPU and memory at the
finest resolution the tab offers. Record the timestamp of each read. If the tab
cannot export, keep timestamped screenshots in the local directory and cite
them; do not transcribe values without a timestamp.

Keep the service list complete: a worker without a row in Settings → Workers is
still a service whose memory must be attributed or explicitly listed as
unmeasured.

### 4.4 Per-sample cross-check

At least once per window, while all six workers are `working`, run on the hub:

```sh
railway ssh --project <project> --environment <environment> \
  --service <hub-service> -- sh -lc 'date -u +%FT%TZ; ao session top --all --json'
```

Record the output with that UTC timestamp. `ao session top` has no
`sampledAt` field, so the paired `date -u` line is the timestamp for the
cross-check, not a per-row value. Entries whose `rssBytes` or `processCount` is
`null` had no live process tree at that read and must be recorded as unmeasured,
not zero. The command is a cross-check of session RSS and process counts; it is
not the primary CPU/process source.

From any machine where the worker hosts are paired, `ao session top --host
<label> --all --json` returns the same shape for that host's daemon, so the hub
and every worker host can be sampled in the same minute with one command.

## 5. Comparison template

Copy this block into the result report and fill every row from samples. Use
`measured` for a direct reading and `inferred` for anything derived. An empty
cell is not allowed; write `not measured` and say why.

```markdown
## CO-1.1.1 resource baseline

### Run metadata

| Field | Value |
| --- | --- |
| Run ID | |
| Workload variant | A (paired hosts) / B (Railway worker services) |
| Window (UTC start–end) | |
| Window length | |
| Sample interval | 60 s |
| Repeat window (UTC start–end) | |
| Hub service memory limit at capture | |
| AO version and commit | |
| Railway deployment IDs (hub and each worker) | |
| Six tasks (issue numbers) | |
| Harnesses per worker | |

### Samples

| Target | Where | Source | Avg RSS | Peak RSS | Avg CPU | Peak CPU | Sample count | Basis |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Hub daemon (non-session) | hub | usage route `app.own` | | | | | | measured |
| Hub orchestrator session | hub | usage route `sessions[]` | | | | | | measured |
| Hub BFF | hub | `ps` | | | | | | measured |
| `Linux` worker 1 (`claude-code`) | Linux | host usage route | | | | | | measured |
| `Linux` worker 2 (`codex`) | Linux | host usage route | | | | | | measured |
| `IMac` worker 1 (`claude-code`) | IMac | host usage route | | | | | | measured |
| `IMac` worker 2 (`codex`) | IMac | host usage route | | | | | | measured |
| `Z600` worker 1 (`claude-code`) | Z600 | host usage route | | | | | | measured |
| `Z600` worker 2 (`codex`) | Z600 | host usage route | | | | | | measured |
| Hub Railway service | hub | Metrics tab | | | | | | measured |
| Each worker Railway service | worker | Metrics tab | | | | | | measured |

### Time near the limit

Define "near" as at or above 90% of the service's configured memory limit.
Report the number of samples and the minutes represented by them; do not convert
a coarse Metrics-tab point into minutes without stating the point interval.

| Service | Memory limit | Samples ≥90% | Minutes near limit | Longest continuous span | Basis |
| --- | --- | --- | --- | --- | --- |
| | | | | | |

### Processes that could not be measured

Every process not covered above must appear here, including any of: a BFF not
reachable through `railway ssh`,
`tailscaled` when enabled, harness grandchildren that detached from the session
tree, processes shorter than the sampling interval, platform or container
overhead that has no process, and AO processes on a host that was offline.

| Process or service | Why it was not measured | Effect on attribution |
| --- | --- | --- |
| | | |

### Repeat variance

| Metric | Window 1 | Window 2 | Delta | Material? |
| --- | --- | --- | --- | --- |
| Peak hub service memory | | | | |
| Peak daemon RSS | | | | |
| Peak orchestrator session RSS | | | | |
| Peak sum of six worker sessions | | | | |
| Minutes near the hub limit | | | | |

Material variance means a peak RSS or service memory difference greater than
20%, or a time-near-limit difference greater than five minutes. If material,
state the likely cause (task mix, build cache, harness update, deployment
restart) and whether it changes the attribution.

> The 23.84 GB service-level maximum against a 24 GB limit recorded in the
> CO-1.1.1 backlog is **unverified**. It appears only in backlog text, cannot be
> attributed to a process, and is not reproduced by this protocol until the run
> above is complete. Do not cite it as a measured baseline.

### Attribution

State which process, if any, accounts for the peak, and whether that conclusion
is measured or inferred. If the process sum is below the Railway service
reading, say by how much and why the remainder is not attributable.
```

## 6. Known limits

- **RSS is not unique memory.** Both the usage route and `ps` sum per-process
  RSS; shared pages are counted once per process. The sum is an upper bound for
  the process tree and a lower bound relative to the container's service
  reading. State which direction the comparison uses.
- **CPU is interval-based.** The route reports share of one core since the
  previous sample; the first sample is zero. At a 60-second interval it is an
  average over that minute, not an instantaneous figure. A service Metrics-tab
  CPU value may use a different unit or window; record the unit.
- **Sessions without a live runtime are absent, never zero.** A session that is
  starting, stopped or unreachable on its host has no row. Treat it as
  unmeasured.
- **The hub cannot see worker process memory.** The orchestrator's remote
  session records carry status, not the remote daemon's process table. Each
  worker host's own route is the only process-level source.
- **The BFF and optional tailscaled are outside the daemon's `app`
  reading.** Missing `ps` coverage for any of them is an explicit gap, not a
  zero.
- **Railway service memory includes the container and platform.** A process sum
  smaller than the service reading is expected; it does not prove the process
  list is complete or incomplete.

## 7. What remains

The operator's remaining action is one run of this protocol with Railway access
and a six-worker workload: preflight, four samplers (hub plus three hosts), the
two Railway usage reads and per-service Metrics reads, the repeat window, and the
filled comparison template. This document and
`scripts/railway/resource-sample.mjs` do not perform the measurement and do not
prove any part of it.

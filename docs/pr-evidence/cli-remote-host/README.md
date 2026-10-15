# Evidence: one orchestrator, workers on other machines (`--host`)

Design: [`docs/superpowers/specs/2026-10-15-cli-remote-host-target-design.md`](../../superpowers/specs/2026-10-15-cli-remote-host-target-design.md).

## Live check: two real daemons

Two `ao daemon` processes built from this branch ran on one macOS machine with
isolated state, so neither touched `~/.ao`:

| Role | `AO_DATA_DIR` | Loopback port | Host ID |
| --- | --- | --- | --- |
| A, the orchestrator's machine | `$S/a/data` | 4201 | `h_61e0128e-…` |
| B, the worker machine | `$S/b/data` | 4101 | `h_ad4fe327-…` |

B's authenticated remote listener was enabled with the stock
`ao remote-host enable` (LAN, `192.168.0.100:3011`). A's `remotes.json` held
one 0600 entry for B in the desktop's format (label `build-box`, address,
password, host ID). The password never appears in any output below.

Both machines registered a clone of the same small public repository with
different project IDs and remote spellings, so a match by ID or by raw string
would fail:

- A: project `hello`, origin `git@github.com:Octocat/Hello-World.git`
- B: project `hello-clone`, origin `https://github.com/octocat/Hello-World.git`,
  plus unrelated projects `other` and `ultra-clone`

A ran with `AO_PROJECT_ID=hello AO_SESSION_ID=hello-orchestrator-1`, the
environment its daemon gives an orchestrator session. `A` and `B` below are the
branch binary with each machine's environment. The full output is in
[`live-transcript.txt`](live-transcript.txt).

### What it shows

1. `ao host ls` verifies B's identity and reports it `online`.
2. `ao spawn --host build-box` with no `--project` picked `hello-clone` by
   repository (`github.com/octocat/hello-world`) and created
   `hello-clone-1` on B: `spawned session hello-clone-1 "remote worker" (idle)
   on host "build-box"`. The agent was Codex in Chat mode with no prompt, so the
   check spent no model tokens.
3. The session exists only on B: `A session ls --host build-box` and
   `B session ls` both list it, plain `A session ls` shows none.
4. `session get`, `session kill`, and `send` reached B. Error envelopes carry
   B's request IDs (`bJpZHwO2gN-…`). The plain send after the kill was refused
   by B's controller with `SESSION_TERMINATED`, which shows it passed B's
   authentication.
5. Every refusal scenario failed closed:
   - wrong saved password → `rejected the saved password … (BAD_PASSWORD)`, exit 1
   - saved host ID ≠ answering host → `identity changed … connection refused`, exit 1
   - an entry pointing at A's own listener → `is this machine`, exit 2
   - `remotes.json` mode 0644 → `chmod 600` guidance, exit 1
   - `ao status --host …` → `unknown flag: --host`, exit 2 (local-only commands
     cannot reach a remote host)

### B's side of the wire

[`host-b-request-log-summary.txt`](host-b-request-log-summary.txt) counts the
requests B's daemon logged from `remote=192.168.0.100`, meaning they arrived
through the authenticated remote listener and not through loopback. It includes
`POST /api/v1/sessions 201`, `GET /api/v1/sessions/hello-clone-1 200`,
`POST …/kill 200`, and the refused `…/send 409`. Over the same run:

- B received **0** `/internal/*` requests from A. Loopback-only routes stayed local.
- A's own daemon received all **14** `/internal/telemetry/cli-invoked` events.
- B's data directory holds the session's artifacts and chat host
  (`$S/b/data/artifacts/hello-clone-1`, `$S/b/data/chat-hosts/hello-clone-1`).

The one `POST /api/v1/sessions 409` in the summary is the first attempt. B
could not fetch a large GitHub repository within the spawn deadline, so the
check switched to the small public repository above. It is an environment
limit, not a routing failure.

### After the review fixes: direct connections under a dead proxy

The branch binary was rebuilt after the review fixes and rerun with
`HTTP_PROXY` and `HTTPS_PROXY` set to `http://127.0.0.1:9`, a dead port. If
the CLI had used the proxy, every call would have failed:

- `host ls` reported `online`.
- `spawn --host` created `hello-clone-2` on B.
- `session ls --host` and `session kill --host` succeeded.

See [`live-transcript-proxy.txt`](live-transcript-proxy.txt).

## Mutation checks

Each guard was broken on purpose and its test was rerun. Every mutation failed
the named test, and the code was restored afterwards:

| Mutation | Caught by |
| --- | --- |
| Forward local `AO_SESSION_ID` as `parentSessionId` | `TestRemoteSpawnTargetsPairedHostAndMatchesProjectByRepository` |
| Follow redirects with the bearer | `TestRemoteRedirectIsNotFollowedWithCredential` |
| Skip the saved-identity comparison | `TestRemoteIdentityMismatchNeverSendsPassword` |
| Send loopback telemetry to the remote host | `TestRemoteSpawnTargetsPairedHostAndMatchesProjectByRepository` |
| Forward local `senderSessionId` | `TestRemoteSendKeepsAttributionInTextOnly` |
| Allow targeting this machine | `TestRemoteTargetRefusesThisMachine` |
| Let the remote client use an HTTP proxy | `TestRemoteHTTPClientNeverUsesAProxy` |
| Fall back from an isolated `AO_DATA_DIR` to `~/.ao/remotes.json` | `TestRemotesFilePathResolution` |
| Drop `--host` from the steering recovery hint | `TestRemoteSteerRecoveryHintKeepsHost` |

## Independent review

A separate reviewer read the diff without this context and found three defects.
All three are fixed and have mutation-checked tests (the last three rows above):

1. **Proxy exposure (medium).** Remote requests inherited `HTTP(S)_PROXY`. A
   proxy could answer the identity probe with the public host ID and then read
   the plaintext bearer. The remote client now always connects directly.
2. **Lab isolation (low–medium).** An isolated `AO_DATA_DIR` without saved hosts
   fell back to the user's real `~/.ao/remotes.json`. The fallback now applies
   only to the desktop's default daemon directories.
3. **Steering recovery hint (low).** The suggested recovery command omitted
   `--host`, so following it would query the local daemon. It now repeats the
   host, shell-quoted.

The reviewer's test-gap notes are also covered:
- a redirect on the identity probe;
- zero or several repository matches;
- routing for every command that opted into `--host`;
- a data race in the redirect test.

## Not covered here

- A cross-machine run (two physical hosts, or a Cloudflare/Tailscale address).
  The code path is the same HTTP client. Only the address differs.
- A successful `ao send` that starts a model turn on the remote worker. It was
  skipped to avoid spending provider tokens. The routing and authentication of
  that exact endpoint are shown by the `SESSION_TERMINATED` refusal, and the
  request body is covered by `TestRemoteSendKeepsAttributionInTextOnly`.

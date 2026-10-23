# Architecture consultation history

The local archive remains canonical evidence. Neo4j is a retained read-only
query index, not a replacement archive. No source files are modified or expired.
The supervisor's **Consultations** navigation opens the list and evidence timeline.

## Enable and backfill

Configure the existing knowledge graph on every host that owns consultations
(see this directory's README). The scanner derives the AO state root from the
parent of the daemon's resolved
`dataDir` and reads only `<parent-of-dataDir>/architecture-consults/archive/`.
For the default `~/.ao/data` this is the skill's canonical
`~/.ao/architecture-consults/archive/`. Scratch and dev daemons use the archive
under their own state root, never the real archive. Existing archives require
no relocation or migration.

Apply `schema.cypher` through the existing schema operation. The scanner also
repeats the five consultation constraints/indexes on its first successful pass.
The writer needs schema permissions; the API uses the existing read credentials.

Scanning starts asynchronously after graph configuration and repeats every 30
seconds. Each pass considers at most 32 directory entries/consultations and has a
10-second deadline. Directory cursors continue catch-up across passes. Each file
has a 32 MiB hashing budget; larger files are reported unreadable and retried.
Content is limited to 256 KiB per artifact and 1 MiB of JSON-escaped text per
consultation, with an
explicit truncation flag and full-file SHA-256 for successfully hashed artifacts.
Symlinks and nonregular artifacts are rejected. Only the exact skill artifacts
for the three named consuls and rounds 1–2, plus the explicitly supported
`fallback-arbitration/fable.md` original arbiter report, are indexed; logs,
prompts and other directories are excluded.

Set `AO_KNOWLEDGE_GRAPH_CONSULTATION_SCAN=0` before starting the daemon to disable
local scanning while retaining graph reads. No new listener or credential is
introduced. Loopback remains local; remote reads use the existing authenticated
remote-host listener. Do not expose the primary listener.

## Metadata and ballots

The manifest projection allows only structured string fields for requester,
case, recorded host, project name, branch, SHA, patch digest, phase, created and
verified timestamps, dedicated decision status and separate process statuses.
It also reads only nested `archive.status`, `delivery.status`, escalation status
and UTC timestamps, and per-consul closure strings (including the fallback
arbiter). Fallback arbitration exposes only status, requested model, effort and
host. Unknown shapes remain unknown. Requester is read only from a structured
`requester` string; `owner`, caller prose and channel objects never supply it.
The skill requires this explicit field for new consultations; historical
archives without it remain Unknown and are never rewritten to infer a caller.
Display fields are bounded to 512 bytes and reject path separators; branch has
its own bounded relative-ref grammar. Status fields accept bounded lowercase
tokens. Raw manifests, session handles, caller channels, message IDs, absolute
archive paths and logs are never indexed. Original Markdown is evidence and can
itself contain sensitive project text. Do not archive credentials. Restrict
graph access accordingly.

Ballots yield structured votes only with this entire-file grammar:

```text
Proposal: proposal-1.md
D01: APPROVE
D03: REJECT
```

Use the exact applicable proposal filename, unique `D` decision IDs with a
bounded alphanumeric suffix (dots, dashes and underscores are allowed, for
example `D639-1`), and `APPROVE` or `REJECT`. Additional prose, duplicate IDs,
mismatched
versions and other formatting produce unknown stance while retaining raw text.
Keep rationale in the original collected reports. This grammar does not infer
required decision IDs, quorum, consensus or final outcomes. Historic ballots are
not rewritten. `fallback_arbitration` metadata and its original Fable report identify
arbitration without council votes. Proposal versions and observed ballot rounds
are counted separately; a proposal file alone does not create a voting round.

## Read API and recovery

`GET /api/v1/knowledge/consultations?limit=25&cursor=...` accepts limits 1–50.
Pagination is newest date first, then stable composite key, with an opaque continuation cursor.
Detail IDs are unpadded base64url of `<daemonHostId>/<UTC-date>/<consultationId>`;
encode the complete key, never put raw slashes in the detail URL.
Reads have a five-second deadline. Responses distinguish 501 (not configured),
503 (index unreachable), 404 (record absent) and 400 (invalid query).

Scan status is retained in daemon memory and a per-host graph node.
The list includes local scanner enablement, last scan time, duration, examined
and imported counts and a sanitized error. These describe this daemon only;
they do not certify other hosts are caught up. Detail shows last successful
sync, missing artifacts, truncated content and unknown states explicitly.
A missing final answer never becomes a synthesized decision. `Last sync` is the
last changed import; unchanged digests skip artifact writes while local scan time
continues to advance.

On graph failure, preserve the archive and restore connectivity/permissions;
periodic scans retry. Reimport uses stable consultation/artifact keys and one
transaction, so retries do not duplicate records. A vanished artifact retains
its last indexed content and first observed absence time. Unsafe or unreadable
evidence is marked unavailable separately, retains any last indexed copy, and
never claims that the source file vanished. Restore its original
file to clear absence on the next successful scan. Restarting scanning resets
directory cursors and backfills again safely. Existing Neo4j backup/restore
covers the index; retain host archives separately. There is no global sync,
Cloud service, deployment, mutation endpoint or automatic retention policy.

## Validation

Normal tests require no network or user archives:

```bash
cd backend
go test -cover ./internal/consultations
go test ./internal/httpd/...
```

Opt-in `kglive` tests use the configured graph and delete only uniquely named
test consultation records. `consultarchive_live` reads the approved local #639
archive without changing it. Run both tags to verify that original archive
through the real graph and the HTTP detail endpoint:

```bash
go test -tags=kglive -run TestLiveIndex -v ./internal/consultations
go test -tags=consultarchive_live -run TestRealArchiveScanner -v ./internal/consultations
go test -tags=kglive,consultarchive_live -run TestRealArchiveGraphAPI -v ./internal/consultations
```

The renderer review fixture at
`frontend/test/fixtures/consultations/index.html` uses the actual page and typed
client with synthetic API responses. It covers list/detail navigation, complete
and fallback evidence, safe Markdown, empty, 501 and 503 states and both themes.
It complements the real archive/graph/API integration; it is not a live daemon.
The corresponding Playwright regression is `frontend/e2e/consultations.spec.ts`.

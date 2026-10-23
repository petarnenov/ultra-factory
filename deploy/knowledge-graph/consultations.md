# Architecture consultation history

The local archive remains canonical evidence. Neo4j is a retained read-only
query index, not a replacement archive. No source files are modified or expired.
The supervisor's **Consultations** navigation opens the list and evidence timeline.

## Enable and backfill

Configure the existing knowledge graph on every host that owns consultations
(see this directory's README). The daemon uses its resolved data directory:
`<AO_DATA_DIR>/architecture-consults/archive/YYYY-MM-DD/<consultation-id>/`.
The default is `~/.ao/data`; hosts using the skill's older `~/.ao/architecture-consults`
location must explicitly copy their archive into that data-directory layout,
retaining originals and verifying hashes. No other roots are searched.

Apply `schema.cypher` through the existing schema operation. The scanner also
repeats the four consultation constraints/indexes on its first successful pass.
The writer needs schema permissions; the API uses the existing read credentials.

Scanning starts asynchronously after graph configuration and repeats every 30
seconds. Each pass considers at most 32 directory entries/consultations and has a
10-second deadline. Directory cursors continue catch-up across passes. Each file
has a 32 MiB hashing budget; larger files are reported unreadable and retried.
Content is limited to 256 KiB per artifact and 1 MiB of JSON-escaped text per consultation, with an
explicit truncation flag and full-file SHA-256 for successfully hashed artifacts.
Symlinks and nonregular artifacts are rejected. Only the exact skill artifacts
for the three named consuls and rounds 1–2 are indexed; logs are excluded.

Set `AO_KNOWLEDGE_GRAPH_CONSULTATION_SCAN=0` before starting the daemon to disable
local scanning while retaining graph reads. No new listener or credential is
introduced. Loopback remains local; remote reads use the existing authenticated
remote-host listener. Do not expose the primary listener.

## Metadata and ballots

Only flat string fields `requester`, `case`, `host`, `created_at`, `phase`,
`decision_status`, `archive_status`, `delivery_status`, `closure_status`,
`escalation_status`, and `mode` are read from `state.json`. Unknown shapes remain
unknown. Display fields are bounded to 512 bytes and reject path separators;
status fields accept bounded lowercase tokens. Raw manifests, session handles,
caller channels, message IDs, absolute archive paths and logs are never indexed.
Original Markdown is evidence and can itself contain sensitive project text.
Do not archive credentials. Graph access must remain restricted accordingly.

Ballots yield structured votes only with this entire-file grammar:

```text
Proposal: proposal-1.md
D01: APPROVE
D03: REJECT
```

Use the exact applicable proposal filename, unique `D` decision IDs of at least
two digits, and `APPROVE` or `REJECT`. Additional prose, duplicate IDs, mismatched
versions and other formatting produce unknown stance while retaining raw text.
Keep rationale in the original collected reports. This grammar does not infer
required decision IDs, quorum, consensus or final outcomes. Historic ballots are
not rewritten. `mode: d03_fallback` identifies arbitration without council votes.

## Read API and recovery

`GET /api/v1/knowledge/consultations?limit=25&cursor=...` accepts limits 1–50.
Pagination is newest date first, then stable composite key, with an opaque continuation cursor.
Detail IDs are unpadded base64url of `<daemonHostId>/<UTC-date>/<consultationId>`;
encode the complete key, never put raw slashes in the detail URL.
Reads have a five-second deadline. Responses distinguish 501 (not configured),
503 (index unreachable), 404 (record absent) and 400 (invalid query).

The list includes local scanner enablement, last scan time, duration, examined
and imported counts and a sanitized error. These describe this daemon only;
they do not certify other hosts are caught up. Detail shows last successful
sync, missing artifacts, truncated content and unknown states explicitly.
A missing final answer never becomes a synthesized decision.

On graph failure, preserve the archive and restore connectivity/permissions;
periodic scans retry. Reimport uses stable consultation/artifact keys and one
transaction, so retries do not duplicate records. A vanished artifact retains
its last indexed content and first observed absence time. Restore its original
file to clear absence on the next successful scan. Restarting scanning resets
directory cursors and backfills again safely. Existing Neo4j backup/restore
covers the index; retain host archives separately. There is no global sync,
Cloud service, deployment, mutation endpoint or automatic retention policy.

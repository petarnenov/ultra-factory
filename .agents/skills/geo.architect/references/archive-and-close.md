# Archive and close a consultation

Use this file archive until a store-backed replacement is implemented. AO may index allowlisted evidence in Neo4j for read-only browsing; that index is not a replacement archive and never authorizes deleting originals. Do not add a database or migrate historical consultations as part of ordinary skill use.

## Stable archive layout

For AO, use `architecture-consults/archive/` under the resolved `AO_DATA_DIR` (default `~/.ao/data`). Otherwise use that relative path under the applicable agent state root. Existing archives at the older `~/.ao/architecture-consults/archive/` location remain canonical at their recorded location; retain originals when explicitly copying them into the index scanner root. The date is the UTC creation date; the ID distinguishes consultations on the same day.

```text
archive/
  YYYY-MM-DD/
    <consultation-id>/
      final.md
      deepseek.md
      astra.md
      fable.md
      request.md
      state.json
      proposal-1.md
      proposal-2.md                 # Only if a second proposal was needed
      independent/{deepseek,astra,fable}.md
      critique/{deepseek,astra,fable}.md
      ballots/round-1/{deepseek,astra,fable}.md
      ballots/round-2/{deepseek,astra,fable}.md
      logs/
```

Keep original artifact names and contents. The three top-level architect files collect that architect's complete analysis, critique and ballots in order, with round labels, proposal versions and relative links to the originals. Do not replace these with shortened summaries. Only create round artifacts that actually exist; on an incomplete consultation, state which outputs are missing in the corresponding architect file. Never manufacture missing votes or reports.

Write artifacts atomically. Before delivery, verify `final.md` and all three architect files are readable and nonempty, original reports are preserved, and their relative links resolve. Record the archive host, absolute path, included files and verification timestamp in `state.json`. Keep decision status separate from archive, delivery and closure status.

When consuls run remotely, collect their finished artifacts into this archive on the caller's host before closing sessions; verify transferred file hashes against the originals. Preserve remote originals if transfer fails. Do not claim the archive is complete or delete source artifacts until verification succeeds. Delivery must state the actual available location and any archiving failure.

## Delivery and closure

1. Finish and verify the archive; publish the decision with its stable archive location.
2. Record delivery acknowledgement from the caller mechanism, then close all three consultation-owned consul sessions. If the only delivery mechanism is a final response that ends the turn, close them after archive verification and immediately before that response; record delivery as unconfirmed rather than inventing an acknowledgement.
3. For each consul, use the owning harness/runtime's supported close or stop operation for the exact recorded session handle. One-shot CLI processes that already exited need only verified exit status; saved conversation history does not count as a running session and must not be deleted. If a separate managed session remains open after its CLI exits, close that session too. Capture session IDs and the supported close mechanism during launch, not after losing access to the runtime.
4. Verify each process exited and each managed session is terminal/closed using its owning runtime. An interrupt request, idle status or disconnected terminal alone is not proof of closure. If graceful shutdown stalls, use only a supported termination operation scoped to that recorded consultation-owned process/session, after rechecking ownership. Never use broad process-name kills or stop shared servers, the caller, or unrelated agents.
5. Update `state.json` with each consul's session/process identity, terminal status, closure timestamp and any error. Finalization is complete only when the archive is verified and all three consuls are confirmed closed or already exited. Report exact remaining handles if closure is unavailable or fails; keep closure pending rather than claiming success.

Apply the same finalization to resolved, unresolved, failed and cancelled consultations: preserve all available evidence, identify missing artifacts honestly, and close every consul that was started. A consul never started is recorded as `not_started`, not as a fabricated closed session. Delivery or archive failure must not leave owned sessions running indefinitely: preserve available artifacts, attempt closure, and report both failures separately.

Closure releases runtime resources; it does not authorize deleting archive files, transcripts or worktrees. Keep the archive without automatic expiry until an explicit retention policy or store migration replaces this temporary arrangement. Reusing an archived decision does not reopen its old sessions; a new consultation receives a new ID and links to the earlier archive when relevant.

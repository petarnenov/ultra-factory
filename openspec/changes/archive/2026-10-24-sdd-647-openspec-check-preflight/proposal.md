# Proposal

## Why

The repository's OpenSpec check assumes Node 24 and the root lockfile-installed executable are available. Unsupported Node currently produces an assertion, while a missing executable reaches a process-spawn error; both fail before contributors receive actionable setup guidance.

## What Changes

- Add deterministic preflight diagnostics for unsupported Node versions and a missing root-pinned OpenSpec executable.
- Keep the existing pinned validation sequence unchanged when setup is valid.
- Add focused tests for both setup failures and the existing success behavior.

## Capabilities

### New Capabilities

- `openspec-check-setup`: Requirements for the repository-local OpenSpec check's runtime and executable preflight and its existing successful validation contract.

### Modified Capabilities

None.

## Impact

This changes the developer-facing behavior of `scripts/openspec/check.mjs` and adds Node tests and one repository-local capability spec. It changes no application runtime, API, dependency, CI gate, or release behavior. The capability describes repository tooling only.

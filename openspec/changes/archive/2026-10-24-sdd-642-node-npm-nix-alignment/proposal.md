# Proposal

## Why

The Nix development shell selects Node 22, while the repository's CI and contributor guidance use Node 24. The npm minimum currently stated in the README is not independently established by CI or lockfile evidence.

## What Changes

- Select the Node 24 package exposed by the pinned nixpkgs input in the Nix development shell.
- Align the README and development guide on Node 24 with the npm bundled with that Node distribution.
- Record that CI selects Node 24 but does not pin npm separately; do not claim a numeric npm minimum or npm 10 compatibility without evidence.
- Document the clean `make setup` flow and Nix version commands, while recording actual Nix shell execution as unverified on this Mac and owned by follow-up issue #664.
- Create and archive this no-runtime OpenSpec change in the same PR.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This developer-tooling change has no runtime behavior or behavior-spec delta.

## Impact

Affected files are `flake.nix`, `README.md`, `docs/development.md`, and this OpenSpec change and its archive. The PR targets `dev` and closes only #642. No CI, package, lockfile, product runtime, Go, other toolchain, release, or deployment behavior changes.

## Scope and Non-goals

This issue owns only the Nix Node package selection, contributor Node/npm guidance, and its no-runtime OpenSpec record. `docs/agents/openspec.md`, CI workflows, package manifests and lockfiles, pnpm selection, Go, cloud tooling, application runtime dependencies, and broader SDD enforcement remain unchanged.

## Requirements and Scenarios

- **R642-01 / S642-01:** The Nix shell selects Node 24, and contributor guidance names the same supported local Node major.
- **R642-02 / S642-02:** npm guidance distinguishes the npm bundled with Node 24 from separately pinned CI npm. No numeric minimum or npm 10 compatibility claim is made without direct evidence.
- **R642-03 / S642-03:** Clean setup and relevant Nix version commands are documented. Actual Nix execution is either verified on macOS or remains explicitly unverified with a named owner and follow-up issue.

## Verification

- Verify the locked nixpkgs source defines `nodejs_24`; source inspection is not a Nix evaluation.
- Strictly validate this change with the lockfile-pinned OpenSpec 1.14.3 CLI through the root npm wrapper, then archive with `--skip-specs` only because no behavior spec delta exists.
- Run lightweight source/docs checks and `git diff --check`.
- Record `nix develop -c node --version` and `nix develop -c npm --version` as unrun on this Mac because Nix is unavailable; Petar Nenov owns follow-up issue #664. No installation is attempted and no pass is claimed.
- Record that Node 24.11.0/npm 11.6.1 and Node 24.21.0/npm 11.19.0 are available locally. npm 10 is unavailable, so npm 10 compatibility is not claimed.

## Risks and Rollback

The exact Nix package attribute is supported by the pinned nixpkgs source, but source evidence cannot establish shell evaluation on macOS. Record the actual prerequisite, affected platform/scenario, owner, and follow-up verification issue under D09. CI's bundled npm is not separately pinned; avoid a numeric npm floor. If the focused toolchain alignment is wrong, revert this PR on `dev`; no data migration or runtime rollback is needed.

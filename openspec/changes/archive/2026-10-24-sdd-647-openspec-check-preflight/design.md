# Design

## Context

`scripts/openspec/check.mjs` is a local script and the existing Node 24 `api-drift` workflow entry point. Its successful path validates the `sdd-01-openspec-bootstrap` sample and checks malformed and repaired local fixtures using the package pinned in the root lockfile.

## Goals

- Fail before any child process when the Node major is not 24 or the pinned executable is missing.
- Give deterministic messages that name the required setup and root-level recovery command.
- Preserve the current successful validation sequence and pinned version assertion.
- Test the preflight paths without network access or invoking OpenSpec.

## Approach

Extract a small callable check function from the current script and inject only the Node version, executable-presence check, and process-spawn function needed for deterministic tests. Keep production defaults bound to `process.versions.node`, filesystem existence, and `spawnSync`; do not add environment overrides. Perform the Node check and executable preflight before the first spawn. A missing executable message points to `npm ci` from repository root. When preflight succeeds, run the existing version and sample/fixture checks in the same order.

Tests stub the spawn function and executable-presence check for both failure scenarios, asserting the exact actionable message and zero spawn calls. The success-path unit test asserts the unchanged command sequence; `npm run openspec:check` provides the real pinned-CLI integration check against the local fixtures.

## Compatibility and security

The change affects only a repository development script. It adds no package or runtime dependency, network call, CI job, gate, credential access, or application behavior. The successful path remains pinned to OpenSpec 1.14.3.

## Archive

After manual evidence review and strict validation, archive this change with the root-pinned OpenSpec 1.14.3 CLI without `--skip-specs`, so the accepted capability spec is synchronized and the completed change is retained.

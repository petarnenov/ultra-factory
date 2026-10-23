# OpenSpec workflow

OpenSpec is a repository development tool. Use Node 24 and install the root lockfile before invoking it:

```sh
npm ci
npm run openspec -- --version
```

The root `package.json` pins `@fission-ai/openspec` exactly. Do not use a global install, `npx`, or a mutable `latest` tag for normal work. The `api-drift` job runs on Node 24, installs the same root lockfile, and runs `npm run openspec:check`.

## CLI equivalents

These are the tested local CLI forms. The `--` passes arguments to the pinned local executable:

| Operation | Command |
| --- | --- |
| Create a change | `npm run openspec -- new change <change-id> --description "<summary>"` |
| Show status | `npm run openspec -- status --change <change-id> --json` |
| Get artifact instructions | `npm run openspec -- instructions proposal --change <change-id> --json` |
| Validate a change | `npm run openspec -- validate <change-id> --strict --no-interactive` |
| Archive a completed tooling or documentation change | `npm run openspec -- archive <change-id> --yes --skip-specs` |

Archive only after the project’s review and completion requirements pass. `--skip-specs` is appropriate only when the change explicitly has no behavior spec delta; it does not skip proposal or review requirements. Slash commands are integrations for specific agents, not portable shell commands. Validate exact options against `npm run openspec -- <command> --help` when the pinned version changes.

## Agent integrations

OpenSpec 1.14.3 generated and this repository checked these profiles:

| Profile | Generated files | Result |
| --- | --- | --- |
| Claude Code (`claude`) | `.claude/commands/opsx/` and `.claude/skills/openspec-*/` | Six commands and six skills created |
| Codex (`codex`) | `.agents/skills/openspec-*/` and `.agents/skills/.openspec-target` | Six skills created; this profile uses skills rather than generated commands |
| OpenCode 2 (`opencode`) | `.opencode/commands/` and `.opencode/skills/openspec-*/` | Six commands and six skills created |

Only these three profiles are initialized here. Generated guidance supplements the repository’s `AGENTS.md`, `docs/agents/`, and task-specific instructions; it does not override them or grant permission to skip their review, security, CI, issue, branch, or integration rules. Existing project instructions were not edited by initialization.

## Provenance and upgrades

At implementation setup on 2026-10-23 20:21:05 UTC, the official npm `latest` tag resolved to `@fission-ai/openspec` 1.14.3. npm metadata identifies upstream repository `https://github.com/Fission-AI/OpenSpec`, release/tag [`v1.14.3`](https://github.com/Fission-AI/OpenSpec/releases/tag/v1.14.3), published 2026-10-17 at commit `c794978f4f8845ce59693373c9dd882ee776588c`. The package declares Node `>=20.19.0`; local validation used Node `v24.21.0` and npm `11.19.0`. Registry tarball: `https://registry.npmjs.org/@fission-ai/openspec/-/openspec-1.14.3.tgz`. Lockfile integrity: `sha512-ALFW1d5bSm6t27Kmcle93SDFuqUc8fLwy5ZdBfIiNffo0Yk9AEOJxw6xDqBv38eMM7l7TeOZBB/7C08luxYGfg==`.

An upgrade is deliberate: prepare a full proposal and receive the required architecture review first, resolve the official npm `latest` tag again, record its timestamp/version/source/Node/integrity evidence, update the exact package and lockfile, inspect regenerated files, and rerun the real and malformed validations. CI uses the committed lockfile and never resolves or installs `latest` automatically.

At setup, `npm audit signatures` reported verified registry signatures for all 90 installed packages and verified attestations for 25 packages. Package integrity confirms the locked tarball bytes; it does not by itself prove publisher identity or source-to-package correspondence.

OpenSpec is a development-only dependency. Application runtime packages, desktop/mobile bundles, Railway images, and release workflows do not depend on it.

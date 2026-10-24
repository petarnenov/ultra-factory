# Design

## Context and scope

This is a developer-tooling and contributor-documentation change with no runtime behavior. CI selects Node 24. The existing flake lock pins nixpkgs revision `e9a7635a57597d9754eccebdfc7045e6c8600e6b`; its package source defines `nodejs_24` as Node 24.15.0 and includes the npm output. This source inspection does not prove Nix evaluation or execution on macOS.

## Decisions

- Replace only `pkgs.nodejs_22` with `pkgs.nodejs_24`; leave pnpm and every other package unchanged.
- Document Node 24 as the supported local Node major and describe npm as bundled with that Node distribution. CI selects Node 24 but does not set an npm version.
- Do not state an npm numeric minimum. The root lockfile has no root `engines` entry, and lockfile version alone is not evidence of npm 10 compatibility. Local Node 24 installations observed here bundle npm 11.6.1 and 11.19.0; those are examples, not pins.
- Use `make setup` from a clean checkout. It runs `npm ci` for the repository's configured lockfiles, downloads Go modules, and runs the doctor checks.
- Keep `docs/agents/openspec.md` unchanged. The implementation PR records the required manual proposal/design/tasks completeness disposition at its exact reviewed head.

## Verification gap and owner

No Nix executable is available on the assigned Mac. No installation or Nix evaluation was attempted. The exact acceptance commands are `nix develop -c node --version` and `nix develop -c npm --version`, to be run on a Nix-capable macOS host. The verification owner is Petar Nenov. Follow-up issue #664 tracks this check; until it passes, the Nix execution scenario remains explicitly unverified and must not be reported as passed or full readiness.

## npm compatibility evidence plan

Do not infer npm 10 compatibility from lockfile version. If a numeric minimum is later required, inspect the official `actions/setup-node@v5` behavior, capture Node/npm versions from a successful Node 24 CI install, and run clean `npm ci` with an actual npm 10 binary against each lockfile in the contributor setup. Record exact versions, paths, commands, and exit codes. Until those checks exist, keep the npm floor unclaimed.

## Risks and rollback

The pinned source identifies `pkgs.nodejs_24`, but the shell remains unevaluated locally. Keep that platform gap open with the named follow-up. npm bundled versions can vary across Node 24 releases, and CI does not pin npm separately; avoid overpromising compatibility. Revert the focused flake, contributor-guide, and OpenSpec files if the alignment proves incorrect.

# Changelog

## [0.16.0] - 2026-10-19

### Features

- **agents:** resolve per-machine spawn config with blocking validation (#260) (ebc36653e)
- **review:** run reviews on a paired host over a verified checkout (#258) (f18c495b3)
- **agents:** resolve spawn config per machine with blocking validation (b74463816)
- **review:** run reviews on a paired host over a verified checkout (5c8588665)

### Bug Fixes

- **agentlaunch:** scope reviewer validation and refresh stale catalogs (9e30def18)
- **i18n:** translate the resolved agent label for every locale (b6881f21c)
- **comments:** keep spawn resolution comments within policy (15a9c7997)
- **review:** release disposable worktrees materialized during restore (0e6680bfd)
- **review:** verify restored checkouts and unverified queue heads (6a9ace585)

### Other Changes

- promote dev into main (#264) (64bfa8cf0)
- bump golangci-lint to v2.14.0 for Go 1.27.2 export data (#262) (701c978ac)
- document resolved agent provenance (b6427a378)
- Merge remote-tracking branch 'origin/dev' into ao/ultra-factory-59/issue-254 (38f70a006)
- Merge remote-tracking branch 'origin/dev' into ao/ultra-factory-50/remote-review (86335c48e)
- record hub orchestrator and worker machine roles (#257) (ee173e637)
- clarify review head SHA precondition (203d08bee)
- record hub orchestrator and worker machine roles (c57e045dd)
- Merge pull request #253 from petarnenov/ao/ultra-factory-36/issue-239 (4d375c5f3)
- **ci:** document release guard trigger narrowing (#239) (f036a97f2)
- note PR builds now run only amd64 worker smoke (#239) (d7069a1bd)
- remove feat/** publish from ADR consequences (#239) (c215b8eaa)
- align self-hosted channel publishing with workflow trigger (#239) (f03ba7dea)
- cut same-commit duplicate runs and redundant PR image build (#239) (0e6f49832)

## [0.15.1] - 2026-10-19

### Bug Fixes

- **opencode:** isolate resolver tests and make two flaky ones deterministic (6ca1a1eb7)

### Other Changes

- Merge pull request #252 from petarnenov/dev (f10ec1e3f)
- Merge remote-tracking branch 'origin/main' into sync/dev-main (b93a74085)
- Merge pull request #250 from petarnenov/fix/opencode-test-isolation (a3a189aef)
- Merge pull request #251 from petarnenov/chore/check-all (69c871414)
- **make:** add check-all for the locally runnable CI checks (556986b4e)
- Merge pull request #249 from petarnenov/chore/agents-dev-flow (fe1650669)
- **agents:** point the Railway orchestrator prompt at the dev flow (6d9ee661c)

## [0.15.0] - 2026-10-19

### Features

- **railway:** automate agent CLI pin updates (#246) (fb865aa00)

## [0.14.0] - 2026-10-18

### Features

- **ci:** bump the version from conventional commits on main (adfb0a74a)

### Other Changes

- Merge pull request #244 from petarnenov/feat/version-bump (3a6f29d35)

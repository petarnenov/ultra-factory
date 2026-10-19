# Changelog

## [0.17.1] - 2026-10-19

### Bug Fixes

- **validation:** validate AO_VITEST_MAX_FORKS (568f1cf91)

### Other Changes

- promote dev into main (#276) (8e102d419)
- **validation:** cap local test parallelism and serialise aggregates (#274) (cc0a0e0aa)
- **validation:** cap local test parallelism (590254e8e)

## [0.17.0] - 2026-10-19

### Features

- **usage:** collect OpenCode 2 token usage from its session store (#273) (65014a831)
- **tasks:** honest completed-task outcomes and a card layout (#271) (ea9f662bb)
- **projects:** bind GitHub accounts to projects with machine-local secrets (#263) (c999cd2e9)
- **usage:** collect OpenCode 2 token usage from its session store (bf97d4b5b)
- **tasks:** honest completed-task outcomes and a card layout (5b05bd4d2)
- **sessions:** detect stalls from progress heartbeats and derived health (#265) (0f958627c)
- **scm:** resolve project identity for reviewer sessions (fcf2e8858)
- **scm:** scope project GitHub identity by machine and host (33d7f8d10)
- **sessions:** derive stall health from progress heartbeats (71f34f7a8)
- **projects:** bind GitHub accounts to projects with machine-local secrets (cdfa1b021)

### Bug Fixes

- **sidebar:** label the aggregate board action "Aggregate Kanban" (#267) (a2cfb3c56)
- **scm:** keep a terminated session's unsettled PRs refreshed (#270) (f651f446f)
- **issue-policy:** normalise CRLF before stripping code spans (a3ab4693e)
- **issue-policy:** stop inline code spans at a blank line (4796bb1e2)
- **issue-policy:** strip inline code spans that wrap a line (edb86dfeb)
- **scm:** inject GH_TOKEN for *.ghe.com tenancy hosts (43a87d665)
- **issue-policy:** drop the workflow_dispatch re-check path (0df35d3c6)
- **cli:** rename secret GitHub host flag to --github-host (6740947b1)
- **scm:** withhold machine GitHub credentials on relaunch (4e32a5650)
- **issue-policy:** require a same-repository head for exemptions (1cb210ed3)
- **scm:** omit GH_TOKEN from enterprise-host sessions (d1bf0ba53)
- **scm:** keep a terminated session's unsettled PRs refreshed (711a5e0a0)
- **issue-policy:** treat inline code as literal text (19a13ef01)
- **sidebar:** label the aggregate board action "Aggregate Kanban" (bc49be121)
- **sessions:** record same-state hook progress and gate recovery reports (49e530cbe)
- **sessions:** address stall-review findings (198c45b09)
- **scm:** neutralize machine host variables for bound projects (0eebd34e3)
- **comments:** keep the notification policy comment within the limit (b4cf384fd)
- **scm:** satisfy lint for secret store and PR account lookup (826df6732)
- **scm:** resolve project accounts for claim and PR actions (37cda25ac)

### Other Changes

- promote dev into main (#275) (c71d6b1fd)
- **issue-policy:** require an issue type and priority label on every pull request (#269) (ca2497e38)
- refresh CI after superseded failures (ce39da276)
- **issue-policy:** note the URL limit and tighten the concurrency note (073b9f24a)
- **issue-policy:** queue runs instead of cancelling them (715f526ec)
- **issue-policy:** say who assigns the priority (36100604b)
- **issue-policy:** describe where the label sets live (11d4b8e30)
- **issue-policy:** name the check run Issue tracking policy (d3cf329d7)
- **issue-policy:** enforce issue type and priority labels on pull requests (89e4e7411)
- **scm:** scope synthetic PAT allowlist to test fixtures (922030df5)
- Merge remote-tracking branch 'origin/dev' into ao/ultra-factory-52/github-account (4df936651)
- **scm:** keep host comments within the repository policy (05367c709)
- **notify:** cover the session stalled notification (aa040e7f4)
- Merge remote-tracking branch 'origin/dev' into ao/ultra-factory-53/issue-259-stall-health (babc104a5)
- **scm:** use obviously fake non-PAT token fixtures (d7ae70551)
- **scm:** keep changed comments within the repository policy (f1a400e07)
- **scm:** allowlist obviously fake PAT fixtures (0199fcf6d)
- Merge remote-tracking branch 'origin/dev' into ao/ultra-factory-52/github-account (ea37a0cab)

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

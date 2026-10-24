## ADDED Requirements

### Requirement: OpenSpec check setup preflight (R647-01, R647-02, R647-03)
The repository-local OpenSpec check MUST verify its required Node major version and root-pinned executable before invoking OpenSpec validation. With supported setup, it MUST report the pinned OpenSpec version and validate its sample and fixture controls.

#### Scenario: Unsupported Node version (S647-01)
- **WHEN** the check runs with a Node major version other than 24
- **THEN** it exits nonzero before invoking OpenSpec and explains that Node 24 is required and how to rerun the root-pinned check

#### Scenario: Missing pinned executable (S647-02)
- **WHEN** the root-pinned OpenSpec executable is missing
- **THEN** the check exits nonzero before attempting validation and directs the contributor to run `npm ci` from the repository root

#### Scenario: Supported setup preserves validation (S647-03)
- **WHEN** the check runs with Node 24 and the pinned executable installed
- **THEN** it validates the bootstrap sample, rejects the malformed fixture for its missing scenario, accepts the repaired control, and reports the pinned OpenSpec version

## Purpose

Defines the command-line error behavior relied on by people and scripts invoking `ao`.

## ADDED Requirements

### Requirement: Distinguish command misuse from runtime failure (R620-CLI-01)
The CLI MUST exit with status 2 for invalid command usage and status 1 for daemon or other runtime failures.

#### Scenario: Invalid usage does not contact the daemon (S620-CLI-01)
- **WHEN** a command is invoked with missing or invalid arguments
- **THEN** it reports a usage error, exits 2, and does not send a request to the daemon

### Requirement: Preserve daemon error code and request identifier (R620-CLI-02)
When a daemon request fails with an API error envelope, the CLI MUST retain the stable error code and request identifier in its user-visible error.

#### Scenario: Daemon error remains actionable in CLI output (S620-CLI-02)
- **WHEN** a daemon response contains an error code and request identifier
- **THEN** the CLI exits 1 and exposes both values with the error message

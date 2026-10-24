# session-lifecycle Specification

## Purpose
Defines how the daemon derives session display status and treats uncertain runtime observations.

## Requirements

### Requirement: Derive display status from durable session facts (R620-SL-01)
The daemon MUST derive display status from durable lifecycle, activity, and related session facts at read time and MUST NOT persist a separate display status.

#### Scenario: List status reflects current durable facts (S620-SL-01)
- **WHEN** the session service returns sessions for display
- **THEN** it derives each display status from durable facts and does not read or write a stored display-status value

### Requirement: Preserve uncertainty in runtime observations (R620-SL-02)
The daemon MUST NOT treat a failed, unknown, or conflicting runtime probe as proof that a session is dead.

#### Scenario: Failed probe leaves session lifecycle unchanged (S620-SL-02)
- **WHEN** a runtime or workload probe fails or returns an unknown result
- **THEN** the daemon records the observation as uncertain and does not terminate the session solely because of that result

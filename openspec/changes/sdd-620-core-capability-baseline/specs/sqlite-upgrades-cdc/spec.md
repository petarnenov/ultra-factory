## Purpose

Defines SQLite upgrade compatibility, migration history, and database change-event behavior.

## ADDED Requirements

### Requirement: Apply forward migrations without rewriting shipped history (R620-DB-01)
SQLite upgrades MUST add new migration history without editing already-shipped migration files and MUST preserve durable rows when applying missing, interleaved, or compatibility-repair schema changes.

#### Scenario: Upgrade retains durable data and migration history (S620-DB-01)
- **WHEN** the daemon opens a database that needs a forward migration or a supported burned-version repair
- **THEN** it applies the required schema change without rewriting earlier migration entries or losing existing durable records

### Requirement: Emit change events from SQLite change records (R620-DB-02)
Durable database changes covered by the change log MUST emit events through SQLite change-log triggers; store methods MUST NOT emit a parallel duplicate change event.

#### Scenario: Durable update produces one trigger-backed change event (S620-DB-03)
- **WHEN** a covered durable record is inserted or updated
- **THEN** SQLite records the corresponding change-log event for the CDC reader without a parallel store-level emission

# sqlite-upgrades-cdc Specification

## Purpose
Defines SQLite upgrade compatibility, migration history, and database change-event behavior.

## Requirements

### Requirement: Keep shipped migration files append-only (R620-DB-01)
SQLite upgrades MUST NOT edit or remove shipped migration files. New schema changes MUST use an unused migration version. Compatibility repair MAY change legacy Goose rows only when schema and recorded history prove the migration was applied. Repair MUST preserve durable data and be atomic and idempotent.

#### Scenario: Upgrade retains durable data without changing shipped files (S620-DB-01)
- **WHEN** the daemon opens a database that needs a new forward migration
- **THEN** it applies a new migration file with an unused version and preserves existing durable records

#### Scenario: Identified legacy history is repaired without losing data (S620-DB-02)
- **WHEN** the daemon opens a database whose physical schema and legacy Goose history unambiguously identify a previously applied migration under a moved version
- **THEN** it may repair the legacy ledger atomically and idempotently while preserving durable records
- **AND** it leaves ambiguous or partial history untouched for explicit error handling
- **AND** it makes no claim that simultaneous independent opens are safe; that behavior is tracked by [#661](https://github.com/petarnenov/ultra-factory/issues/661)

### Requirement: Emit change events from SQLite change records (R620-DB-02)
Durable database changes covered by the change log MUST emit events through SQLite change-log triggers; store methods MUST NOT emit a parallel duplicate change event.

#### Scenario: Durable update produces one trigger-backed change event (S620-DB-03)
- **WHEN** a covered durable record is inserted or updated
- **THEN** SQLite records the corresponding change-log event for the CDC reader without a parallel store-level emission

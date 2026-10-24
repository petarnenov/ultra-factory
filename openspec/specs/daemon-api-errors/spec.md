# daemon-api-errors Specification

## Purpose
Defines the daemon's stable error response contract for API clients and operators.

## Requirements

### Requirement: Return typed API errors with request correlation (R620-API-01)
For a non-success response, the daemon MUST return a stable JSON error envelope containing the error kind, machine-readable code, safe message, and request identifier when available.

#### Scenario: Typed service error preserves its contract (S620-API-01)
- **WHEN** a request fails with a typed validation, not-found, conflict, or unavailable error
- **THEN** the response uses the corresponding HTTP status and stable envelope fields, including the request identifier

### Requirement: Redact unexpected internal error details (R620-API-02)
For an unexpected internal failure, the daemon MUST return a generic safe message and MUST NOT expose internal diagnostic details in the response while preserving request correlation.

#### Scenario: Unexpected failure is safe for clients (S620-API-02)
- **WHEN** a handler receives an unclassified internal error
- **THEN** the client receives a generic internal-error envelope with the request identifier and no raw internal cause

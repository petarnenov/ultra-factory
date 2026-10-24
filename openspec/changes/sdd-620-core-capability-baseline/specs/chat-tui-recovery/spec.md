## Purpose

Defines durable recovery and ownership-fencing behavior for Chat and TUI session controllers.

## ADDED Requirements

### Requirement: Recover accepted Chat work without duplicate dispatch (R620-CR-01)
The daemon MUST retain accepted Chat work across controller or daemon restart and MUST NOT redispatch work whose acceptance is already durable. An uncertain outcome MUST remain distinguishable from a confirmed failure or completion.

#### Scenario: Accepted work is recovered once after restart (S620-CR-01)
- **WHEN** the controller or daemon restarts after accepting a Chat action
- **THEN** recovery returns the durable action outcome without dispatching the same provider action again

#### Scenario: Uncertain work remains recoverable (S620-CR-03)
- **WHEN** the daemon cannot establish whether a reserved Chat action completed
- **THEN** it retains the action as uncertain and does not blindly redispatch it or resurrect a closed conversation

### Requirement: Fence controller signals by current ownership (R620-CR-02)
The daemon MUST reject stale TUI or Chat signals whose controller generation, interface epoch, or ownership no longer matches the session's current controller.

#### Scenario: Stale controller signal cannot mutate the current session (S620-CR-02)
- **WHEN** a delayed signal arrives from a controller made stale by a Chat/TUI handoff or generation change
- **THEN** the daemon leaves the current session lifecycle and controller state unchanged

## Purpose

Defines safe workspace setup, recovery, and cleanup behavior for registered project worktrees.

## ADDED Requirements

### Requirement: Preserve dirty work during ordinary cleanup (R620-WL-01)
Ordinary workspace cleanup MUST refuse to remove a dirty registered worktree and MUST preserve its files and registration. This requirement covers ordinary cleanup only. D06 prohibits force-deleting dirty registered worktrees; direct uncaptured `ForceDestroy` is a known defect tracked by #659, and no preserve-before-force exception is approved.

#### Scenario: Dirty workspace cleanup is refused (S620-WL-01)
- **WHEN** ordinary cleanup attempts to destroy a registered worktree containing uncommitted work
- **THEN** it reports the dirty-workspace refusal and preserves the worktree contents and registration, even if the associated session is terminated

### Requirement: Resume workspace setup only for a matching checkout (R620-WL-02)
After restart, workspace setup recovery MUST resume only when the persisted workspace identity matches the requested checkout and MUST refuse unsafe parent-worktree reuse.

#### Scenario: Restart recovery verifies checkout identity (S620-WL-02)
- **WHEN** workspace setup resumes after a process restart
- **THEN** it continues only for the matching checkout and refuses a parent worktree that cannot safely serve as the requested child workspace

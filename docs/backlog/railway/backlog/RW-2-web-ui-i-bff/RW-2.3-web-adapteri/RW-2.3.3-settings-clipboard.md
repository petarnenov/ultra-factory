# RW-2.3.3 · Task: Settings in localStorage; clipboard and openExternal

**Type:** frontend TS. **Estimate:** 0.5–1 pd. **Story:** RW-2.3.

## Context
- `uiSettings` (2), `keybindings` (3), `updateSettings` are stored by main in `~/.ao`. On the web there is no such path; per-browser localStorage is acceptable (it is not daemon app state).
- `clipboard` (20 refs) → `navigator.clipboard`; `openExternal` → `window.open(url, "_blank", "noopener")`. The stubs already do this; check the edge cases (HTTP, no permission).

## Subtasks
- [ ] Stub `uiSettings.get/set`, `keybindings.get/set/reset` on top of localStorage with try/catch and a default on error.
- [ ] `clipboard.readText` in a browser requires permission and a user gesture: on denial show a toast, not an error.
- [ ] Tests.

# RW-3.3.3 · Task: "Remove worker" dialog

**Type:** frontend React. **Estimate:** 0.5–1 pd. **Story:** RW-3.3.

## Subtasks
- [ ] On open: `GET /hosts/:id/api/v1/sessions` → shows the active sessions; a warning that the volume and worktrees will be deleted irreversibly.
- [ ] Confirmation by typing the label; checkbox "Force (kill running sessions)" for `?force=1`.
- [ ] Submit → `DELETE /hub/workers/:id` → status removing → disappears from the list. 409 → shows the sessions and suggests force.
- [ ] For `source: manual` the dialog only removes the record and says so clearly.
- [ ] Vitest.

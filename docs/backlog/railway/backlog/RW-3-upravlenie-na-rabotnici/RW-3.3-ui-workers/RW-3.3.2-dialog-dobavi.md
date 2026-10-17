# RW-3.3.2 · Task: "Add worker" dialog
**Status:** ⬜ todo

**Type:** frontend React. **Estimate:** 0.5–1 pd. **Story:** RW-3.3.

## Subtasks
- [ ] Two tabs: "Create in Railway" (label, size/region if the provider supports it, Claude auth: API key or OAuth token, GitHub token, git name/email) and "Add existing by address" (label, URL, password → RW-2.4.3 API).
- [ ] Validation: label `[a-z0-9-]{3,32}` unique; at least one Claude credential; `GH_TOKEN` format.
- [ ] Submit → `POST /hub/workers` → closes the dialog, the list shows provisioning. Errors 429 (limit) and 5xx with a readable message.
- [ ] Secret fields are `type=password`, without autocomplete, and are cleared on close.
- [ ] Vitest: validation and submit payload.

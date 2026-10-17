# RW-3.3 · Story: "Workers" UI screen
**Status:** ✅ done

**As** an operator **I want** to see the workers with their status in Settings → Workers and add/remove them through a dialog, **so that** I can manage capacity without the Railway console.

**Estimate:** 1.5–3 pd. **Dependencies:** RW-3.2, RW-2.4.4. Read `DESIGN.md`; use `components/ui/*` (shadcn).

| Task | Name | pd |
|---|---|---|
| RW-3.3.1 | List with statuses, progress and errors | 0.5–1 |
| RW-3.3.2 | "Add worker" dialog (form, validation, secrets) | 0.5–1 |
| RW-3.3.3 | "Remove" dialog with active sessions and confirmation by name | 0.5–1 |

**Acceptance criteria**
- [ ] Statuses update without a reload (polling `GET /hub/hosts` every 5 s while anything is provisioning/removing, otherwise 30 s).
- [ ] Secrets in the form are never shown after submit and are never logged.
- [ ] The screen is available only when `isWebBuild` (in Electron, Remote hosts stays as it is).

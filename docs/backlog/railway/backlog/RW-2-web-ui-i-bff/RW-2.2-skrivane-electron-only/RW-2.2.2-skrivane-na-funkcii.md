# RW-2.2.2 · Task: Hiding Electron-only features

**Type:** frontend React. **Estimate:** 1–1.5 pd. **Story:** RW-2.2. **Dependencies:** RW-2.2.1.

## Context
- Read `DESIGN.md` before making changes.
- Affected: `BrowserPanel.tsx` (2046 LOC) and the inspector rail Browser tab; `editorHandoff` buttons (10 refs); `cliLink` settings; `updates`/`featureBuilds` (Settings); `WindowTitlebar.tsx`; tray/dock badge; the "Choose directory" native dialog in import project (URL clone `/api/v1/projects/clone` remains instead); `terminal.saveDroppedFile` drag-drop.

## Subtasks
- [ ] Inspector rail: Browser tab hidden when `!capabilities.browserPanel`; `ao preview` events show an "open in new tab" link instead of the panel.
- [ ] Settings: the Updates, CLI link and Browser profiles sections are hidden on web; the Remote hosts section stays (it is used by the BFF registry in RW-2.4.3).
- [ ] Session view / artifacts: "Open in Cursor/VS Code" → hidden; "Copy path" stays.
- [ ] Import project: on web show only "Clone from URL"; native folder picker hidden.
- [ ] Terminal drag-drop: on web show a toast "File upload is not available on the web" (or leave it for the RW-4 upload endpoint).
- [ ] `WindowTitlebar`: do not render on web; `_shell` layout without the titlebar offset.
- [ ] Vitest snapshot/behaviour tests for every hidden surface with an `isWebBuild` mock.

## Acceptance criteria
- [ ] Walking through all 16 routes in the web build without console errors and without empty containers.
- [ ] `npm run typecheck && npm run test` green.

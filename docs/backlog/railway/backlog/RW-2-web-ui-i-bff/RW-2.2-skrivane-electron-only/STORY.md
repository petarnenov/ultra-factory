# RW-2.2 · Story: Hiding Electron-only features in the web build
**Status:** ✅ done

**As** a browser user **I want** the buttons and panels that require Electron to be hidden or to show a clear message, **so that** there are no broken actions.

**Estimate:** 1.5–2 pd. **Dependencies:** RW-2.1.3.

| Task | Name | pd |
|---|---|---|
| RW-2.2.1 | Capability flag `isWebBuild` and audit of direct `window.ao` usages | 0.5 |
| RW-2.2.2 | Hiding: browser panel, editor handoff, CLI link, updates, tray, window chrome | 1–1.5 |

**Acceptance criteria**
- [ ] No runtime errors from `window.ao.*` in the web build (the console is clean when walking through all routes).
- [ ] The Electron build is visually unchanged.
- [ ] `DESIGN.md` is respected: hidden elements leave no empty gaps or broken layout.

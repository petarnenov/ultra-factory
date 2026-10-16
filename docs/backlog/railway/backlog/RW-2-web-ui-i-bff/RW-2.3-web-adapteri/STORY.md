# RW-2.3 · Story: Web adapters for shortcuts, notifications, settings, clipboard

**As** a browser user **I want** keyboard shortcuts, notifications and UI settings to work, **so that** it is usable day to day.

**Estimate:** 1.5–3 pd. **Dependencies:** RW-2.2.1.

| Task | Name | pd |
|---|---|---|
| RW-2.3.1 | Keyboard shortcuts in the renderer (today they come from main IPC) | 0.5–1 |
| RW-2.3.2 | Web Notifications and badge in `document.title` | 0.5 |
| RW-2.3.3 | `uiSettings`/`keybindings` in localStorage; clipboard/openExternal check | 0.5–1 |

**Acceptance criteria**
- [ ] ⌘/Ctrl+N, ⌘/Ctrl+T, navigation between sessions and the terminal shortcuts work in the browser; they are not duplicated in Electron.
- [ ] A notification for a finished session is shown (after permission); the title shows the unread count.
- [ ] Settings survive a reload.

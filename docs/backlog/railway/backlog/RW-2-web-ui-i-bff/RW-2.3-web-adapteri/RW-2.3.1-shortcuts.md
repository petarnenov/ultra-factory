# RW-2.3.1 · Task: Keyboard shortcuts in the renderer

**Type:** frontend TS. **Estimate:** 0.5–1 pd. **Story:** RW-2.3.

## Context
- ~12 `app.on*Shortcut` listeners in `preload.ts` are triggered by Electron menu accelerators (`main/menu.ts`, `app-shortcuts.ts`). In the web stub they are no-ops.
- The `keybindings` group (3 methods) and `keybinding-settings.ts` in main.

## Subtasks
- [ ] `lib/web-shortcuts.ts`: a global `keydown` handler when `isWebBuild`, which maps the combinations from the current keybindings list to the same callbacks that the `aoBridge.app.on*Shortcut` stub registers.
- [ ] Avoid conflicts with the browser (⌘T/⌘N/⌘W are reserved in some browsers): document alternatives (Alt+…) and make them the web default in the keybinding defaults.
- [ ] Test: simulated keydown events in jsdom trigger the callbacks.

## Acceptance criteria
- [ ] The shortcut help screen shows the web combinations.

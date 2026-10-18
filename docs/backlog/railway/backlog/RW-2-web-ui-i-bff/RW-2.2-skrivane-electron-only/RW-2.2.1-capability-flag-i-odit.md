# RW-2.2.1 · Task: Capability flag and audit of direct `window.ao` usages
**Status:** ✅ done (#70)

**Type:** frontend TS. **Estimate:** 0.5 pd. **Story:** RW-2.2.

## Context
- 9 files use `window.ao` directly (65 refs, 56 of them `window.ao.browser`): `main.tsx`, `BrowserPanel.tsx`, `BrowserProfileButton.tsx`, `TerminalPane.tsx`, `WindowTitlebar.tsx`, `SessionView.tsx`, `InstallDependencyDialog.tsx`, `hooks/useBrowserView.ts`, `bridge.ts`.
- 86 files go through `aoBridge` (stubbed).

## Subtasks
- [ ] `frontend/src/renderer/lib/platform.ts`: `export const isWebBuild = import.meta.env.VITE_AO_WEB === "1"; export const hasElectronBridge = typeof window !== "undefined" && !!window.ao;` + a `capabilities` object (`browserPanel`, `editorHandoff`, `cliLink`, `autoUpdate`, `nativeDialogs`, `tray`, `windowChrome`).
- [ ] Replace the direct `window.ao.*` usages with `aoBridge` or a `hasElectronBridge` guard in the 8 files (excluding `bridge.ts`).
- [ ] An ESLint rule (`no-restricted-globals`/custom) against `window.ao` outside `bridge.ts`.
- [ ] Test: `platform.test.ts`.

## Acceptance criteria
- [ ] `grep -rn "window.ao" frontend/src/renderer --include=*.ts --include=*.tsx | grep -v bridge.ts | grep -v test` → 0 lines.

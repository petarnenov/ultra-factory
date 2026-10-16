# RW-2.2.1 · Таск: Capability флаг и одит на директните `window.ao` употреби

**Тип:** frontend TS. **Оценка:** 0.5 чд. **Стори:** RW-2.2.

## Контекст
- 9 файла ползват `window.ao` директно (65 реф., 56 от тях `window.ao.browser`): `main.tsx`, `BrowserPanel.tsx`, `BrowserProfileButton.tsx`, `TerminalPane.tsx`, `WindowTitlebar.tsx`, `SessionView.tsx`, `InstallDependencyDialog.tsx`, `hooks/useBrowserView.ts`, `bridge.ts`.
- 86 файла минават през `aoBridge` (стъбовано).

## Събтаскове
- [ ] `frontend/src/renderer/lib/platform.ts`: `export const isWebBuild = import.meta.env.VITE_AO_WEB === "1"; export const hasElectronBridge = typeof window !== "undefined" && !!window.ao;` + `capabilities` обект (`browserPanel`, `editorHandoff`, `cliLink`, `autoUpdate`, `nativeDialogs`, `tray`, `windowChrome`).
- [ ] Замени директните `window.ao.*` с `aoBridge` или guard `hasElectronBridge` в 8-те файла (без `bridge.ts`).
- [ ] ESLint правило (`no-restricted-globals`/custom) срещу `window.ao` извън `bridge.ts`.
- [ ] Тест: `platform.test.ts`.

## Критерии за приемане
- [ ] `grep -rn "window.ao" frontend/src/renderer --include=*.ts --include=*.tsx | grep -v bridge.ts | grep -v test` → 0 реда.

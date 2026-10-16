# RW-2.3.1 · Таск: Клавишни комбинации в renderer-а

**Тип:** frontend TS. **Оценка:** 0.5–1 чд. **Стори:** RW-2.3.

## Контекст
- ~12 `app.on*Shortcut` listener-и в `preload.ts` се задействат от Electron menu accelerators (`main/menu.ts`, `app-shortcuts.ts`). В web stub-а са no-op.
- `keybindings` група (3 метода) и `keybinding-settings.ts` в main.

## Събтаскове
- [ ] `lib/web-shortcuts.ts`: глобален `keydown` handler при `isWebBuild`, който мапва комбинациите от текущия keybindings списък към същите callback-и, които stub-ът на `aoBridge.app.on*Shortcut` регистрира.
- [ ] Избягвай конфликти с браузъра (⌘T/⌘N/⌘W са резервирани в някои браузъри): документирай алтернативи (Alt+…) и ги направи default за web в keybinding defaults.
- [ ] Тест: симулирани keydown събития в jsdom задействат callback-ите.

## Критерии за приемане
- [ ] Shortcut help екранът показва web комбинациите.

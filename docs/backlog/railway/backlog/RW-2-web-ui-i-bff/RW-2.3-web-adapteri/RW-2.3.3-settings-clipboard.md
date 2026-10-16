# RW-2.3.3 · Таск: Настройки в localStorage; clipboard и openExternal

**Тип:** frontend TS. **Оценка:** 0.5–1 чд. **Стори:** RW-2.3.

## Контекст
- `uiSettings` (2), `keybindings` (3), `updateSettings` се пазят от main в `~/.ao`. В web няма такъв път; per-browser localStorage е приемливо (не е app state на daemon-а).
- `clipboard` (20 реф.) → `navigator.clipboard`; `openExternal` → `window.open(url, "_blank", "noopener")`. Стъбовете вече правят това, провери граничните случаи (HTTP, без permission).

## Събтаскове
- [ ] Stub `uiSettings.get/set`, `keybindings.get/set/reset` върху localStorage с try/catch и default при грешка.
- [ ] `clipboard.readText` в браузър изисква permission и user gesture: при отказ покажи toast, не грешка.
- [ ] Тестове.

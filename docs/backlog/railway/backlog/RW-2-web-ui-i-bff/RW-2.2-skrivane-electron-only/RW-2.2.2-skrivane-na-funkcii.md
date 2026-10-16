# RW-2.2.2 · Таск: Скриване на Electron-only функции

**Тип:** frontend React. **Оценка:** 1–1.5 чд. **Стори:** RW-2.2. **Зависимости:** RW-2.2.1.

## Контекст
- Прочети `DESIGN.md` преди промените.
- Засегнати: `BrowserPanel.tsx` (2046 LOC) и inspector rail Browser tab; `editorHandoff` бутони (10 реф.); `cliLink` настройки; `updates`/`featureBuilds` (Settings); `WindowTitlebar.tsx`; tray/dock badge; „Choose directory“ native dialog в import project (вместо него остава URL clone `/api/v1/projects/clone`); `terminal.saveDroppedFile` drag-drop.

## Събтаскове
- [ ] Inspector rail: Browser tab скрит при `!capabilities.browserPanel`; `ao preview` събитията показват линк „отвори в нов таб“ вместо панел.
- [ ] Settings: секции Updates, CLI link, Browser profiles скрити при web; Remote hosts секцията остава (ползва се от BFF registry в RW-2.4.3).
- [ ] Session view / artifacts: „Open in Cursor/VS Code“ → скрит; „Copy path“ остава.
- [ ] Import project: при web показвай само „Clone from URL“; native folder picker скрит.
- [ ] Terminal drag-drop: при web показвай toast „Качването на файлове не е налично в уеб“ (или остави за RW-4 upload endpoint).
- [ ] `WindowTitlebar`: не рендерирай при web; `_shell` layout без отместване за titlebar.
- [ ] Vitest snapshot/behaviour тестове за всяка скрита повърхност с `isWebBuild` mock.

## Критерии за приемане
- [ ] Обход на всички 16 маршрута в web build без конзолни грешки и без празни контейнери.
- [ ] `npm run typecheck && npm run test` зелени.

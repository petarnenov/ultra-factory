# RW-2.1.3 · Таск: Vite web build target

**Тип:** build. **Оценка:** 0.5–1 чд. **Стори:** RW-2.1.

## Контекст
- `frontend/scripts/dev-web.mjs` пуска Vite с `VITE_NO_ELECTRON=1` (mock). Web build-ът НЕ трябва да сетва този флаг.
- `frontend/vite.renderer.config.ts` dev proxy за `/api`, `/mux` към `AO_DEV_API_TARGET` (:112-128).
- Router ползва hash history (`router.tsx:15`), статичен host е ОК.

## Събтаскове
- [ ] `frontend/package.json`: `"build:web": "VITE_AO_WEB=1 vite build --config vite.renderer.config.ts --outDir dist-web"` и `"dev:web:real": "VITE_AO_WEB=1 vite --config vite.renderer.config.ts"` (без mock, с proxy към реален daemon).
- [ ] В `vite.renderer.config.ts`: `base: "./"` при web, `outDir` параметризиран, proxy включва `/readyz`, `/healthz`.
- [ ] `.gitignore`: `frontend/dist-web/`.
- [ ] Документирай в `AGENTS.md` Commands секцията (един ред).

## Критерии за приемане
- [ ] `npm --prefix frontend run build:web` минава; `npx serve dist-web` + proxy показва реален board.

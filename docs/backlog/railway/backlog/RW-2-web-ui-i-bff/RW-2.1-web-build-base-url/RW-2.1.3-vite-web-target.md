# RW-2.1.3 · Task: Vite web build target
**Status:** ✅ done (#64)

**Type:** build. **Estimate:** 0.5–1 pd. **Story:** RW-2.1.

## Context
- `frontend/scripts/dev-web.mjs` runs Vite with `VITE_NO_ELECTRON=1` (mock). The web build MUST NOT set this flag.
- `frontend/vite.renderer.config.ts` dev proxy for `/api`, `/mux` to `AO_DEV_API_TARGET` (:112-128).
- The router uses hash history (`router.tsx:15`), so a static host is OK.

## Subtasks
- [ ] `frontend/package.json`: `"build:web": "VITE_AO_WEB=1 vite build --config vite.renderer.config.ts --outDir dist-web"` and `"dev:web:real": "VITE_AO_WEB=1 vite --config vite.renderer.config.ts"` (no mock, with a proxy to a real daemon).
- [ ] In `vite.renderer.config.ts`: `base: "./"` for web, parameterized `outDir`, the proxy includes `/readyz`, `/healthz`.
- [ ] `.gitignore`: `frontend/dist-web/`.
- [ ] Document it in the Commands section of `AGENTS.md` (one line).

## Acceptance criteria
- [ ] `npm --prefix frontend run build:web` passes; `npx serve dist-web` + proxy shows a real board.

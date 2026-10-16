# RW-1.2.3 · Таск: CI build и publish на worker image-а

**Тип:** CI. **Оценка:** 0.5–1 чд. **Стори:** RW-1.2.

## Цел
GitHub Actions workflow, който билдва `deploy/railway/worker/Dockerfile` за amd64/arm64 и го публикува в GHCR при таг и при промяна на съответните пътища.

## Контекст
- `.github/workflows/` съдържа текущите CI дефиниции; `AGENTS.md` иска локално огледало с `npx @redwoodjs/agent-ci run --all` (Docker).
- `AGENTS.md` „Distribution“: само release conductor публикува desktop артефакти. Worker image-ът е нов канал: тагвай с `worker-v<semver>` и `sha-<short>`, не с desktop версиите, за да не се смесват.

## Събтаскове
- [ ] `.github/workflows/worker-image.yml`: triggers `push` на `main` за пътища `backend/**`, `deploy/railway/worker/**`, `frontend/scripts/build-acp-runtime.mjs`; `push` на тагове `worker-v*`; `workflow_dispatch`.
- [ ] `docker/setup-qemu-action`, `setup-buildx-action`, `login-action` към `ghcr.io` с `GITHUB_TOKEN`, `build-push-action` с cache `type=gha`.
- [ ] Smoke job: пусни image-а с временен volume, провери `/api/v1/identity` на :3011 → 200.
- [ ] Запиши image URL и тагването в `deploy/railway/worker/README.md`.
- [ ] Добави workflow-а към agent-ci локалната проверка, ако е приложимо.

## Критерии за приемане
- [ ] `ghcr.io/<org>/ao-worker:sha-<x>` и `:worker-v0.1.0` съществуват и smoke job-ът е зелен.

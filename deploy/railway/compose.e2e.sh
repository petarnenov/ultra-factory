#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
fixture=$(mktemp -d)
cleanup() {
	rm -rf deploy/railway/e2e-fixture.git "$fixture"
	docker compose -f deploy/railway/compose.e2e.yaml down --volumes --remove-orphans
}
trap cleanup EXIT
export AO_WEB_PASSWORD_HASH="$(printf '%s' 'e2e-operator-password' | npm --prefix packages/hub-bff run --silent hash-password)"
git init -q "$fixture"
git -C "$fixture" config user.email e2e@example.invalid
git -C "$fixture" config user.name "AO E2E"
printf '# AO E2E fixture\n' > "$fixture/README.md"
git -C "$fixture" add README.md
git -C "$fixture" commit -qm "Add local e2e fixture"
git clone -q --bare "$fixture" deploy/railway/e2e-fixture.git
chmod -R a+rX deploy/railway/e2e-fixture.git
docker build -f deploy/railway/worker/Dockerfile --build-arg SKIP_AGENT_INSTALL=1 --build-arg CURSOR_AGENT_VERSION= -t ao-worker:rw-e2e .
docker compose -f deploy/railway/compose.e2e.yaml up --build --detach --wait
npm --prefix frontend exec -- playwright install chromium
if ! npm --prefix frontend exec -- playwright test --config=frontend/playwright.railway.config.ts; then
	docker compose -f deploy/railway/compose.e2e.yaml logs --no-color worker-1
	exit 1
fi

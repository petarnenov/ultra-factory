# RW-3.1.2 · Task: `railway-client.ts`
**Status:** ✅ done (#85)

**Type:** Node TS. **Estimate:** 1–1.5 pd. **Story:** RW-3.1. **Dependencies:** RW-3.1.1.

## Subtasks
- [ ] `WorkerProvider` interface (modeled on `cloud/internal/sandbox/provider.go:181`): `create(spec) → {serviceId, volumeId}`, `getStatus(serviceId) → provisioning|running|failed|stopped`, `setVariables`, `redeploy`, `delete(serviceId, volumeId)`. Railway is the first implementation; the interface allows a different provider in the future.
- [ ] GraphQL queries as constants, `fetch` with `Authorization: Bearer $AO_HUB_RAILWAY_TOKEN`, retry with backoff on 429/5xx, timeout 30 s.
- [ ] Spec: `{name, image, env: Record<string,string>, volumeMountPath: "/data", volumeSizeGb?, region?}`.
- [ ] Errors: typed (`RailwayAuthError`, `RailwayQuotaError`, `RailwayNotFound`), with no token leaking into the messages.
- [ ] Tests with fixtures from RW-3.1.1 (msw or a fake fetch): success, 429 retry, 401, nonexistent service.

## Acceptance criteria
- [ ] Integration test (manual, `RAILWAY_TOKEN` env, skipped in CI): create → status running → delete against `ao-lab`.

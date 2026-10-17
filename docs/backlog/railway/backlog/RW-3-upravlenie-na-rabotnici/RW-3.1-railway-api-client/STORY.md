# RW-3.1 · Story: Railway API client in the BFF
**Status:** ✅ done

**As** a developer **I want** a thin, tested client for the Railway GraphQL API covering the create/delete service, volume, variables and deploy status operations, **so that** the lifecycle in RW-3.2 does not need to know about GraphQL.

**Estimate:** 1.5–2 pd. **Dependencies:** RW-2.4.1.

| Task | Name | pd |
|---|---|---|
| RW-3.1.1 | Research and spike against a real Railway project (recorded protocol) | 0.5 |
| RW-3.1.2 | `railway-client.ts` with typed operations and tests with recorded responses | 1–1.5 |

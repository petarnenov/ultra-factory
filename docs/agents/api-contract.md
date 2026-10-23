# API contract changes

Loaded on demand from [`AGENTS.md`](../../AGENTS.md). Read it before changing a daemon HTTP request or response shape.

The daemon API is code-first. The OpenAPI spec and frontend TypeScript types are generated artifacts. Edit the source, then regenerate.

**Source files to edit:**

- `backend/internal/httpd/controllers/dto.go`: request/response shapes.
- `backend/internal/httpd/apispec/specgen/build.go`: operation registry. Add a `schemaNames` entry for any new named type.

**Regenerate after editing:**

```bash
npm run api          # runs api:spec then api:ts in sequence
```

This is equivalent to running:

```bash
npm run api:spec     # cd backend && go generate ./internal/httpd/apispec/...
npm run api:ts       # npx openapi-typescript@7.4.4 backend/internal/httpd/apispec/openapi.yaml -o frontend/src/api/schema.ts
```

**Verify:**

```bash
cd backend && go test ./internal/httpd/...    # spec drift + route/spec parity tests (does not cover schema.ts; the api-drift CI job checks it)
```

Commit `openapi.yaml` and `frontend/src/api/schema.ts` together with the Go changes. CI will regenerate both files and fail if the committed versions are out of date. The CLI hand-mirrored DTOs remain a deliberate manual boundary and are not generated.

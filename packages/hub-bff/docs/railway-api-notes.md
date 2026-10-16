# Railway public API spike (RW-3.1.1)

Tested on 2026-10-16 in giving-success / production. **Resources were manually deleted by the user; API deletion and worker-image
timing remain unverified.** The existing ultra-factory
service was never modified. All IDs below are masked fixture placeholders.

## Transport and token

Endpoint confirmed: https://backboard.railway.com/graphql/v2.
Requests used curl with Content-Type: application/json and
Project-Access-Token: <MASKED>; never Authorization: Bearer for a project token.
The token was read into a shell variable from the supplied local file, never
printed or stored in this document. Request format:

```sh
curl --silent --show-error --max-time 40 \
  https://backboard.railway.com/graphql/v2 \
  -H "Project-Access-Token: $TOKEN" \
  -H 'Content-Type: application/json' --data '<JSON request below>'
```

A project token is scoped to a project environment according to the
[official API documentation](https://docs.railway.com/integrations/api).
It was sufficient for creation and status operations here, but **not sufficient
for deletion**. The minimal account/workspace permissions for deletion remain
unverified; do not assume successful creation implies cleanup permission.

With explicit user authorization, the installed Railway CLI credentials were also
checked using Authorization: Bearer <MASKED>. Both user.token and user.accessToken
returned Not Authorized on the pre-delete project query; a direct service query
with user.accessToken was also denied. These failures do not establish whether
the credentials were expired, invalid, or lacked access to this project. No
credential refresh or further credential substitution was attempted. No delete
mutation was attempted with these CLI credentials.

## Executed requests and responses

Every GraphQL request below returned HTTP 200, including authorization failures.
Clients must inspect errors, not just the HTTP status.

### Confirm token context

```json
{"query":"{ projectToken { projectId environmentId } }"}
{"data":{"projectToken":{"projectId":"<PROJECT_ID>","environmentId":"<ENVIRONMENT_ID>"}}}
```

### 1. Create service from Docker image

Executed at 16:37:51 UTC. The supplied worker image was not published; used the
authorized public image traefik/whoami instead.

```json
{"query":"mutation($input: ServiceCreateInput!) { serviceCreate(input: $input) { id name } }","variables":{"input":{"projectId":"<PROJECT_ID>","environmentId":"<ENVIRONMENT_ID>","name":"ao-rw-spike-rw-3-1-1","source":{"image":"traefik/whoami"}}}}
{"data":{"serviceCreate":{"id":"<SERVICE_ID>","name":"ao-rw-spike-rw-3-1-1"}}}
```

[Official service examples](https://docs.railway.com/integrations/api/manage-services).

### 2. Create and mount volume

Executed at 16:38:15 UTC. Railway generated the prefixed volume name.

```json
{"query":"mutation($input: VolumeCreateInput!) { volumeCreate(input: $input) { id name } }","variables":{"input":{"projectId":"<PROJECT_ID>","environmentId":"<ENVIRONMENT_ID>","serviceId":"<SERVICE_ID>","mountPath":"/data"}}}
{"data":{"volumeCreate":{"id":"<VOLUME_ID>","name":"ao-rw-spike-rw-3-1-1-volume"}}}
```

This verifies API acceptance of the mount configuration, not filesystem persistence
inside the running container.
[Official volume examples](https://docs.railway.com/integrations/api/manage-volumes).

### 3. Set service variables

```json
{"query":"mutation($input: VariableUpsertInput!) { variableUpsert(input: $input) }","variables":{"input":{"projectId":"<PROJECT_ID>","environmentId":"<ENVIRONMENT_ID>","serviceId":"<SERVICE_ID>","name":"AO_RW_SPIKE","value":"true","skipDeploys":true}}}
{"data":{"variableUpsert":true}}
```

No shared variables were set. This non-secret variable belonged only to the
spike service, which the user subsequently deleted manually. skipDeploys avoids a redeploy;
runtime application of the variable was not tested.
[Official variable examples](https://docs.railway.com/integrations/api/manage-variables).

### 4. Target port and public domain

```json
{"query":"mutation($input: ServiceDomainCreateInput!) { serviceDomainCreate(input: $input) { id domain } }","variables":{"input":{"serviceId":"<SERVICE_ID>","environmentId":"<ENVIRONMENT_ID>","targetPort":80}}}
{"data":{"serviceDomainCreate":{"id":"<DOMAIN_ID>","domain":"ao-rw-spike-rw-3-1-1-production.up.railway.app"}}}
```

A domains query subsequently confirmed targetPort: 80. GET / on the generated HTTPS
domain returned HTTP 200 with the whoami response at 16:38:51 UTC.
[Official domain examples](https://docs.railway.com/integrations/api/manage-domains).

### 5. Read deployment status and timing

```json
{"query":"query { serviceInstance(serviceId:\"<SERVICE_ID>\", environmentId:\"<ENVIRONMENT_ID>\") { serviceName latestDeployment { id status createdAt } } }"}
{"data":{"serviceInstance":{"serviceName":"ao-rw-spike-rw-3-1-1","latestDeployment":{"id":"<DEPLOYMENT_ID>","status":"DEPLOYING","createdAt":"2026-10-16T16:38:18.706Z"}}}}
```

A later execution returned the same deployment with status SUCCESS. First observed
HTTP 200 was 60 seconds after the service-create response. This is an upper bound
from manual checks, includes volume configuration and domain setup, and is not a
precise startup benchmark. Worker-image timing is deferred until RW-1.2.3 publishes
the image to GHCR.

### 6. Delete volume: denied

Before deletion, a project volumes query re-confirmed the returned ID and name
ao-rw-spike-rw-3-1-1-volume.

```json
{"query":"mutation { volumeDelete(volumeId:\"<VOLUME_ID>\") }"}
{"errors":[{"message":"Not Authorized","locations":[{"line":1,"column":12}],"path":["volumeDelete"],"extensions":{"code":"INTERNAL_SERVER_ERROR"},"traceId":"<TRACE_ID>"}],"data":null}
```

### 7. Delete service: denied

After the denial, only cleanup of the already-created service was attempted.
A service query immediately beforehand confirmed its ID and prefixed name.

```json
{"query":"mutation { serviceDelete(id:\"<SERVICE_ID>\") }"}
{"errors":[{"message":"Not Authorized","locations":[{"line":1,"column":12}],"path":["serviceDelete"],"extensions":{"code":"INTERNAL_SERVER_ERROR"},"traceId":"<TRACE_ID>"}],"data":null}
```

The initial spike stopped after these denials. Later, the user explicitly
authorized checking the installed CLI credentials as described above; those
checks were also denied.

## Cleanup state

The domain was successfully removed before the denied volume deletion:

```json
{"query":"mutation { serviceDomainDelete(id:\"<DOMAIN_ID>\") }"}
{"data":{"serviceDomainDelete":true}}
```

The user subsequently confirmed manual deletion of both the spike service and
volume. This is user-confirmed cleanup, not an independently verified API deletion.
The service-scoped variable belonged only to the deleted service; no shared
variables were created. Names were re-checked to start with ao-rw-spike- before
each attempted deletion. The existing ultra-factory service was never modified.

API deletion remains unverified: the recorded serviceDelete and volumeDelete
responses are authorization-error fixtures, not successful cleanup fixtures.
RW-3.1.2 must not interpret these errors as deletion success. Successful deletion
fixtures and the minimum working token permissions require a later authorized
test. Do not run the full sequence unattended until cleanup permissions are
established.

## Verification and remaining work

- Recorded successful service creation, volume creation with a mount path,
  variable upsert, public domain/target port configuration, deployment status
  reads, HTTP reachability, and domain deletion.
- Recorded denied volumeDelete and serviceDelete requests; successful API
  deletion has not been demonstrated.
- The user confirmed manual deletion of the two remaining spike resources.
- Worker-image create-to-online timing is deferred until RW-1.2.3 publishes it
  to GHCR. The public-image observation above is not a worker-image benchmark.
- Minimum working deletion permissions, the applicable project service-count
  limit, and this project's maximum volume size remain unverified.
- **Open question — volume size:** no verified Railway API operation; `volumeCreate`
  accepts `mountPath`, `environmentId` and `region` only; revisit with an
  authorized token.
- The task file has no separate Verification command section. This PR changes
  documentation only; git diff --check is the local whitespace check. Product
  test suites and make check are not required for this docs-only change.

## Limits

Observed on the context, service-create and volume-create responses:

```text
RateLimit-Policy: "default";q=1000;w=3600
Access-Control-Expose-Headers: RateLimit-Policy,X-RateLimit-Limit,X-RateLimit-Remaining,X-RateLimit-Reset,Retry-After
```

The exposed-header list is not evidence that each header is present. No
X-RateLimit-Limit, Remaining, Reset or Retry-After value was returned in these
recorded responses. No throttling or exhaustion test was performed.

The [official API page](https://docs.railway.com/integrations/api) documents
hourly limits of 100 (Free), 1,000 (Hobby), 10,000 (Pro), and custom Enterprise
limits; per-second limits are 10 for Hobby and 50 for Pro. A 429 response should
honor Retry-After. The observed policy alone does not verify the account plan.

The [official volume reference](https://docs.railway.com/volumes/reference)
documents per-project volume counts: Free 1, Trial 3, Hobby 10, Pro 20
(increasable), Enterprise unlimited. Same-service volumes in another environment
do not count toward that limit. Default sizes are 0.5 GB (Free/Trial), 5 GB
(Hobby), and 50 GB (Pro); Pro and above support self-service growth to 1 TB,
with Enterprise required beyond that. These are documentation facts, not limits
tested by resource exhaustion. The applicable maximum for this particular project,
and its service-count limit, remain unverified.

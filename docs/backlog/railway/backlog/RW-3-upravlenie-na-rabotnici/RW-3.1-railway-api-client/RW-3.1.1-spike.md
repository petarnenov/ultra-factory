# RW-3.1.1 · Task: Railway public API spike
**Status:** ✅ done (#81)

**Type:** research. **Estimate:** 0.5 pd. **Story:** RW-3.1.

## Subtasks
- [ ] Read the Railway public API documentation (GraphQL endpoint `https://backboard.railway.com/graphql/v2` at the time of writing; confirm). Record: how to create a service from a Docker image in a given environment, how to create and mount a volume, how to set variables, how to set the target port/public domain, how to read the deployment status, how to delete a service and a volume.
- [ ] Check which token type is required (project token vs account/team token) and the minimal scope for the operations above.
- [ ] Check limits: rate limit, number of services/volumes per project, maximum volume size.
- [ ] Perform the operations manually with `curl` against the `ao-lab` project and record the working requests + responses (with masked ids) in `packages/hub-bff/docs/railway-api-notes.md`. They become fixtures for the tests in RW-3.1.2.
- [ ] Record the time from create to online for a service from the worker image.

## Acceptance criteria
- [ ] The document covers all 7 operations with actually executed requests.

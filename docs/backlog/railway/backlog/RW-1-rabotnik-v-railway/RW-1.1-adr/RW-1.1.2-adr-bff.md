# RW-1.1.2 · Task: ADR for a BFF in front of the daemon orchestrator in the hub

**Type:** ADR. **Estimate:** 0.5–1 pd. **Story:** RW-1.1.

## Goal
Decide how the web UI on Railway reaches the hub's daemon without violating "primary listener stays loopback and unauthenticated".

## Context
- `AGENTS.md` hard rules: "Do not add any other network-facing bind."
- Two options:
  - **B1:** A BFF (Node, in the same container) proxies to `127.0.0.1:3001`. The daemon is not touched. The public surface is the BFF with its own login. Loopback-gated routes become available to a logged-in user.
  - **B2:** The hub daemon also enables the remote-host listener, and the BFF talks to it on :3011 with Bearer, like with any other worker. The blocked routes are lost (`lan_listener.go:67-78`): system install, codex accounts, side-chat launch, desktop, browser.
- `remote-proxy.ts` in the Electron main process is a precedent for "a trusted process on the same machine that injects a credential".

## Subtasks
- [ ] Describe both options with a table of the available routes.
- [ ] Make the decision. Recommendation: **B2 by default** (same path for the hub and the workers, zero change in the daemon), with an explicit list of the lost features; B1 only if that list is unacceptable.
- [ ] Define the BFF login model: a single operator with a password + session cookie (MVP), with OIDC as an option.
- [ ] Define where the credential for each host lives: `hosts.json` on the hub's volume, 0600, in a format compatible with `remotes.json` (`label`, `url`, `password`, `hostId`).
- [ ] Write `docs/adr/000N-web-bff-for-hub.md`.

## Acceptance criteria
- [ ] The ADR fixes the option, the auth model, and the list of features not available on the web.
- [ ] RW-2.4 can be implemented without new decisions.

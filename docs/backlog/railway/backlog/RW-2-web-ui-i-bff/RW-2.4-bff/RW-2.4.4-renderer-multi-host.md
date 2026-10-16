# RW-2.4.4 · Task: Renderer multi-host through the BFF instead of Electron `remotes`

**Type:** frontend TS. **Estimate:** 0.5–1 pd. **Story:** RW-2.4. **Dependencies:** RW-2.4.3, RW-2.2.1.

## Context
- `aoBridge.remotes` (11 methods, 30 refs) today calls Electron main (`remotes-store.ts`, `remote-proxy.ts`) and returns a loopback proxy URL for each host. `lib/host-clients.ts` `clientForHost(hostId)` builds an `apiClient` from that URL.
- Routes `host.$hostId.*` and `RemoteHostsSettings.tsx`.

## Subtasks
- [ ] Web stub for `aoBridge.remotes`: `list` → `GET /hub/hosts`; `add` → `POST /hub/hosts`; `remove` → `DELETE`; `probe` → `POST .../probe`; `baseUrlFor(hostId)` → `"/hosts/<id>"` (same-origin prefix).
- [ ] `host-clients.ts`: in web mode a host's base URL is a prefix, not an absolute URL; `muxUrlFromApiBase` and the SSE construction work with a prefix (check `terminal-mux.ts:72-77`).
- [ ] `RemoteHostsSettings.tsx`: the same UI, with data coming from the stub; on web hide the fields that make no sense (tunnel, LAN addresses).
- [ ] Tests for the stub and for `host-clients` with a prefix.

## Acceptance criteria
- [ ] In the web build: Settings → Remote hosts shows the worker, the `host/<id>` route opens its board, the terminal works.

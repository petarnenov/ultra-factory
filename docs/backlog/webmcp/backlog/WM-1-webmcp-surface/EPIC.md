# WM-1 · Epic: WebMCP shim and page tools

**Status:** ⬜ todo — [#469](https://github.com/petarnenov/ultra-factory/issues/469)

**Goal.** Give the renderer a `document.modelContext`-shaped tool registry and
register tools on the Board, the global Settings overlay, and the Session chat
surface that cover the state transitions the selected Playwright scenarios
exercise.

**Estimate:** 3.5–4 pd. **Stage:** surface. **Dependencies:** none.

| Story | Name | Estimate |
|---|---|---:|
| WM-1.1 | Runtime shim with a test-only call surface | 1 pd |
| WM-1.2 | Tools on the three benchmark pages | 2.5–3 pd |

**Decisions (resolved 2026-10-21)**

- Own shim (no polyfill dependency); target name `document.modelContext`.
- Tools are registered in **all builds**; `window.__aoWebMcp` (list/call) is
  exposed in dev/e2e builds only.

**Acceptance criteria**

- [ ] `document.modelContext.registerTool` works in `dev:web` and in the
  packaged Electron app without a native WebMCP implementation.
- [ ] When the browser provides a native `document.modelContext`, the shim
  forwards registrations to it and keeps the local registry for the test
  surface.
- [ ] Each surface registers its tools on mount and unregisters on unmount;
  `listTools()` elsewhere returns none of them.
- [ ] Every tool has a JSON-schema `inputSchema`, a one-sentence
  `description`, and returns a structured result the runners assert on
  without reading the DOM.
- [ ] No tool performs a destructive or network action beyond what the
  equivalent user click does.

**Risks.** Tools exist in production builds: each tool must be a thin wrapper
over an existing user action, with preconditions enforced by the same guards
the UI uses. No tool exposes secrets, tokens, or raw API responses.

**Out of scope.** Declarative (form-annotation) WebMCP; creating a task
through a tool (`TaskComposer.createTask` is not in any selected scenario);
agent discovery outside the test harness.

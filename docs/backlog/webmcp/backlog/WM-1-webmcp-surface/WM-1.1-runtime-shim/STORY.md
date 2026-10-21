# WM-1.1 · Story: Runtime shim with a test-only call surface

**As** a test author **I want** one module that exposes
`document.modelContext.registerTool` in every AO build **so that** page tools
are written once and work in Chromium 130 today and native WebMCP later.

**Estimate:** 1 pd. **Dependencies:** none.

| Task | Name | Estimate |
|---|---|---:|
| WM-1.1.1 | `document.modelContext` shim and `__aoWebMcp` test surface | 1 pd |

**Acceptance criteria**

- [ ] The shim is the only file that knows whether the native API exists.
- [ ] `window.__aoWebMcp.listTools()` and `callTool(name, args)` are available
  only in dev/e2e builds (`import.meta.env.DEV || usesPreviewWorkspaceData`).
- [ ] `callTool` validates `args` against the tool's `inputSchema` and rejects
  unknown tools with a typed error.

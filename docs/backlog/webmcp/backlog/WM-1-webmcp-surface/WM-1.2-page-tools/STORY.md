# WM-1.2 · Story: Tools on the three benchmark pages

**As** a test author **I want** the Board, Settings overlay, and Session chat
surfaces to register WebMCP tools that cover every step of the selected
Playwright scenarios **so that** the same scenarios can be driven without
selectors.

**Estimate:** 2.5–3 pd. **Dependencies:** WM-1.1.1.

| Task | Name | Estimate |
|---|---|---:|
| WM-1.2.1 | Board tools (`/#/projects/<projectId>`) | 1 pd |
| WM-1.2.2 | Settings tools (global settings overlay) | 0.5–1 pd |
| WM-1.2.3 | Session chat tools (`/#/projects/<p>/sessions/<s>`) | 1 pd |

**Acceptance criteria**

- [ ] For every scenario in the WM-2.1.1 catalog, every step maps to exactly
  one tool call or one bridge step; the catalog lists the mapping.
- [ ] Each surface exposes a `*.read` tool returning the state the scenarios
  assert on, and a `*.wait_for*` tool backed by the same store the UI renders
  from, so drivers never scrape the DOM.
- [ ] Tools call the handlers the UI uses and contain no new business logic.
- [ ] Tools are registered in every build; they are absent from the registry
  when their surface is not mounted.

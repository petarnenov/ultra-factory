# WM-2.1 · Story: Scenario catalog mapped 1:1 from Playwright

**As** a benchmark author **I want** one catalog that lists each selected
Playwright test as tool-level steps **so that** all three drivers run the same
thing and the comparison is fair.

**Estimate:** 1 pd. **Dependencies:** WM-1.2.

| Task | Name | Estimate |
|---|---|---:|
| WM-2.1.1 | Scenario catalog and shared assertion layer | 1 pd |

**Acceptance criteria**

- [ ] The catalog is data (`scenarios.ts`), not test code; drivers interpret
  it.
- [ ] Each scenario has `page`, `route`, `source: {file, title}`, `tools` steps, phase/action candidates, and
  shared assertion entries that reference tool read results, not selectors.
- [ ] Scenarios that depend on layout (wrapping, scroll position) are listed
  under "excluded" with the reason.

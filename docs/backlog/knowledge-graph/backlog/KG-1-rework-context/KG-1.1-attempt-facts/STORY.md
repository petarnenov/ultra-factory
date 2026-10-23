# KG-1.1 · Story: Earlier attempts carry PR, branch and failure facts

**As** a worker reworking an issue **I want** to see where the previous
attempt left its code and what blocked it **so that** I continue from the
branch and fix the real blocker instead of starting over.

**Estimate:** 1.5 pd. **Dependencies:** none.

| Task | Name | Estimate |
|---|---|---:|
| KG-1.1.1 | PR URL and branch in earlier attempts | 0.5 pd |
| KG-1.1.2 | CI and human review failure facts for unmerged attempts | 1 pd |

**Acceptance criteria**

- [ ] A respawn on an issue with an unmerged earlier attempt shows the
  attempt's PR, branch, failed checks and open human review comments.

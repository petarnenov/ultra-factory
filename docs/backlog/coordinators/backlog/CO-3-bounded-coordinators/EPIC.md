# CO-3 · Epic: Run two bounded coordinators

**Status:** ✅ done — CO-3.1.1, CO-3.2.1 and CO-3.3.1 are merged ([#330](https://github.com/petarnenov/ultra-factory/pull/330), [#331](https://github.com/petarnenov/ultra-factory/pull/331), [#335](https://github.com/petarnenov/ultra-factory/pull/335)) and issue [#207](https://github.com/petarnenov/ultra-factory/issues/207) is closed.

**Goal.** Let the root start and supervise two coordinator sessions. Each
coordinator owns no more than three active workers, for a maximum of six
workers in the initial topology.

**Estimate:** 3–5 pd. **Stage:** pilot. **Dependencies:** CO-2.2.

| Story | Name | Estimate |
|---|---|---:|
| CO-3.1 | Spawn coordinators and enforce capacity | 1–2 pd |
| CO-3.2 | Coordinate worker groups and return summaries | 1–2 pd |
| CO-3.3 | Inspect coordinator hierarchy | 1 pd |

**Acceptance criteria**

- [ ] The root can start up to two helpers for distinct, bounded work scopes.
- [ ] A helper can start up to three workers; attempts over either limit
  return a clear capacity error.
- [ ] The root receives summaries and can reassign work after a helper fails.
- [ ] The operator can inspect root → coordinator → worker ownership and state.
- [ ] The root remains the only session allowed to merge or close project-wide
  work.

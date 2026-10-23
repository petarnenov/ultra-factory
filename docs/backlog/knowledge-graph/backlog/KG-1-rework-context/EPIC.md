# KG-1 · Epic: Rework context in earlier attempts

**Status:** ⬜ todo — [#551](https://github.com/petarnenov/ultra-factory/issues/551)

**Goal.** A worker that picks up an issue sees, for each earlier attempt, the
PR and branch to continue from and why the attempt did not merge, from facts
the daemon already stores.

**Estimate:** 1.5 pd. **Stage:** daemon. **Dependencies:** none; KG-1.1.2
builds on KG-1.1.1.

| Story | Name | Estimate |
|---|---|---:|
| KG-1.1 | Earlier attempts carry PR, branch and failure facts | 1.5 pd |

**Acceptance criteria**

- [ ] "Earlier attempts on this issue" lists the PR URL and branch per attempt.
- [ ] An unmerged attempt lists its failed CI checks and unresolved human
  review comments, clipped and behind the trust boundary.
- [ ] The section stays within `maxSectionText` (6000 characters).

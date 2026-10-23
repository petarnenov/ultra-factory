# KG-2 · Epic: New-feature context from where similar tasks changed code

**Status:** ⬜ todo — [#555](https://github.com/petarnenov/ultra-factory/issues/555)

**Goal.** A worker starting a new feature sees, for each similar past task,
its PR and the main files it changed, so it can open them and follow the
same pattern.

**Estimate:** 1 pd. **Stage:** daemon. **Dependencies:** none; ordered after KG-1.

| Story | Name | Estimate |
|---|---|---:|
| KG-2.1 | Similar tasks show where they changed code | 1 pd |

**Acceptance criteria**

- [ ] "Similar past tasks" lists PR and up to 5 changed paths per task.
- [ ] Changed paths take part in task embeddings.

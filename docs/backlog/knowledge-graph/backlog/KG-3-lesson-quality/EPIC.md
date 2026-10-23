# KG-3 · Epic: Lesson quality

**Status:** ⬜ todo — [#558](https://github.com/petarnenov/ultra-factory/issues/558)

**Goal.** Wrong or obsolete lessons stop being served: workers can flag a
served lesson as wrong, and a lesson can be retired once its cause is fixed.

**Estimate:** 1 pd. **Stage:** daemon, then data. **Dependencies:** KG-0.1.1
for the retirement pass.

| Story | Name | Estimate |
|---|---|---:|
| KG-3.1 | Workers flag wrong lessons; fixed lessons retire | 1 pd |

**Acceptance criteria**

- [ ] `ao knowledge rate <id> wrong` lowers a lesson's rank.
- [ ] Retired lessons are never served or returned by search.
- [ ] Lessons whose tooling cause is fixed are retired.

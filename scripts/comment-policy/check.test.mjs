import test from 'node:test';
import assert from 'node:assert/strict';
import { mapDeletedCommentRanges, parseDiff, scanTS, summary } from './check.mjs';

function stats(source, file = 'fixture.ts') {
  return summary(scanTS(source, file));
}

test('accepts two comment lines at exactly 80 columns', () => {
  const source = `// ${'x'.repeat(77)}\n// ${'y'.repeat(77)}`;
  assert.deepEqual(stats(source), { blocks: 0, longLines: 0 });
});

test('rejects a comment line at 81 columns', () => {
  assert.deepEqual(stats(`// ${'x'.repeat(78)}`), { blocks: 0, longLines: 1 });
});

test('groups adjacent line comments as one logical comment', () => {
  const source = '// first\n// second\n// third';
  assert.deepEqual(stats(source), { blocks: 1, longLines: 0 });
});

test('groups consecutive block comments as one logical comment', () => {
  assert.deepEqual(stats('/* first */\n/* second */\n/* third */'), { blocks: 1, longLines: 0 });
});

test('checks block comments by physical line and line count', () => {
  assert.deepEqual(stats('/* first\n * second */'), { blocks: 0, longLines: 0 });
  assert.deepEqual(stats('/* first\n * second\n * third */'), { blocks: 1, longLines: 0 });
  assert.deepEqual(stats(`/* ${'x'.repeat(78)} */`), { blocks: 0, longLines: 1 });
});

test('checks inline comments and ignores comment-like string contents', () => {
  assert.deepEqual(stats(`const value = 1; // ${'x'.repeat(70)}`), { blocks: 0, longLines: 1 });
  assert.deepEqual(stats('const value = "// this is string data";'), { blocks: 0, longLines: 0 });
  assert.deepEqual(stats('const value = `// ${value}`;'), { blocks: 0, longLines: 0 });
  assert.deepEqual(stats('// trailing comment'), { blocks: 0, longLines: 0 });
  assert.deepEqual(summary(scanTS('const view = <span>/* text */</span>;', 'fixture.tsx')), { blocks: 0, longLines: 0 });
});

test('collects comments before closing tokens and JSX braces', () => {
  const block = 'function run() {\n  work();\n  // first\n  // second\n  // third\n}';
  const jsx = 'const view = <div>{/* first\n * second\n * third */}</div>;';
  const object = 'const value = {\n  key: 1,\n  // first\n  // second\n  // third\n};';
  assert.deepEqual(summary(scanTS(block)), { blocks: 1, longLines: 0 });
  assert.deepEqual(summary(scanTS(jsx, 'fixture.tsx')), { blocks: 1, longLines: 0 });
  assert.deepEqual(summary(scanTS(object)), { blocks: 1, longLines: 0 });
});

test('maps changed old comment ranges across deleted lines', () => {
  const comments = [{ start: 10, end: 14 }];
  const deletions = [{ oldStart: 11, oldEnd: 12, newStart: 11, oldCount: 2, newCount: 0 }];
  assert.deepEqual(mapDeletedCommentRanges(comments, deletions), [[10, 12]]);
});

test('does not attribute deleted-file hunks or lose renamed-file paths', () => {
  const diff = [
    'diff --git a/deleted.ts b/deleted.ts',
    '--- a/deleted.ts',
    '+++ /dev/null',
    '@@ -1,3 +0,0 @@',
    '-// old',
    '-// comment',
    '-// group',
    'diff --git a/renamed.ts b/renamed.ts',
    '--- /dev/null',
    '+++ b/renamed.ts',
    '@@ -0,0 +1,3 @@',
    '+// new',
    '+// comment',
    '+// group',
    'diff --git a/kept.ts b/kept.ts',
    '--- a/kept.ts',
    '+++ b/kept.ts',
    '@@ -4,2 +4 @@',
    '-// changed',
    '-// line',
    '+// replacement',
  ].join('\n');
  const changes = parseDiff(diff);
  assert.equal(changes.has('deleted.ts'), false);
  assert.deepEqual(changes.get('renamed.ts').added, [[1, 3]]);
  assert.deepEqual(changes.get('kept.ts').deletions, [
    { oldStart: 4, oldEnd: 5, newStart: 4, oldCount: 2, newCount: 1 },
  ]);
});

test('excludes anchored license notices only', () => {
  assert.deepEqual(stats('// Copyright holder\n// license terms'), { blocks: 0, longLines: 0 });
  assert.equal(scanTS('// This note mentions SPDX-License-Identifier: only.').length, 1);
  assert.deepEqual(stats('// SPDX-License-Identifier: MIT'), { blocks: 0, longLines: 0 });
});

test('keeps semantic TypeScript directives out of ordinary comment debt', () => {
  assert.deepEqual(stats(`// @ts-expect-error ${'x'.repeat(90)}`), { blocks: 0, longLines: 0 });
});

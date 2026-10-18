import test from 'node:test';
import assert from 'node:assert/strict';
import { scanTS, summary } from './check.mjs';

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
  assert.deepEqual(summary(scanTS('const view = <span>/* text */</span>;', 'fixture.tsx')), { blocks: 0, longLines: 0 });
});

test('keeps semantic TypeScript directives out of ordinary comment debt', () => {
  assert.deepEqual(stats(`// @ts-expect-error ${'x'.repeat(90)}`), { blocks: 0, longLines: 0 });
});

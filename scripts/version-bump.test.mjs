import test from 'node:test';
import assert from 'node:assert/strict';
import { bumpLevel, nextVersion, parseCommit, prependEntry, renderEntry } from './version-bump.mjs';

test('parseCommit reads type, scope, and a bang breaking marker', () => {
  assert.deepEqual(parseCommit('feat(api)!: drop the v1 route'), {
    type: 'feat',
    scope: 'api',
    breaking: true,
    description: 'drop the v1 route',
  });
});

test('parseCommit reads a plain conventional commit', () => {
  assert.deepEqual(parseCommit('fix: guard the empty case'), {
    type: 'fix',
    scope: '',
    breaking: false,
    description: 'guard the empty case',
  });
});

test('parseCommit reads a BREAKING CHANGE footer', () => {
  const commit = parseCommit('chore: rework config', 'BREAKING CHANGE: the key moved');
  assert.equal(commit.breaking, true);
  assert.equal(commit.type, 'chore');
});

test('parseCommit falls back for a non-conventional subject', () => {
  assert.deepEqual(parseCommit('Merge pull request #1'), {
    type: 'other',
    scope: '',
    breaking: false,
    description: 'Merge pull request #1',
  });
});

test('bumpLevel maps feat to minor and fix to patch', () => {
  assert.equal(bumpLevel([parseCommit('feat: add a screen')]), 'minor');
  assert.equal(bumpLevel([parseCommit('fix: stop the crash')]), 'patch');
  assert.equal(bumpLevel([parseCommit('perf: cache the list')]), 'patch');
});

test('bumpLevel ignores docs, chore, ci, test, and refactor', () => {
  const commits = ['docs: x', 'chore: y', 'ci: z', 'test: w', 'refactor: v'].map((s) => parseCommit(s));
  assert.equal(bumpLevel(commits), null);
});

test('bumpLevel takes the highest level across commits', () => {
  const commits = [parseCommit('fix: a'), parseCommit('feat: b'), parseCommit('docs: c')];
  assert.equal(bumpLevel(commits), 'minor');
});

test('bumpLevel ranks a breaking change above feat', () => {
  const commits = [parseCommit('feat: a'), parseCommit('fix!: b')];
  assert.equal(bumpLevel(commits), 'major');
});

test('nextVersion steps patch, minor, and major', () => {
  assert.equal(nextVersion('0.13.0', 'patch'), '0.13.1');
  assert.equal(nextVersion('0.13.4', 'minor'), '0.14.0');
  assert.equal(nextVersion('0.13.4', 'major'), '1.0.0');
});

test('nextVersion rejects a non-dotted version', () => {
  assert.throws(() => nextVersion('dev', 'patch'), /unsupported version/);
});

test('renderEntry groups commits under their sections', () => {
  const commits = [
    { type: 'feat', scope: 'api', breaking: false, description: 'add a route', short: 'abc1234' },
    { type: 'fix', scope: '', breaking: false, description: 'stop the crash', short: 'def5678' },
    { type: 'docs', scope: '', breaking: false, description: 'tidy the readme', short: 'aaa1111' },
  ];
  const entry = renderEntry('0.14.0', '2026-10-19', commits);
  assert.match(entry, /^## \[0\.14\.0\] - 2026-10-19\n/);
  assert.match(entry, /### Features\n\n- \*\*api:\*\* add a route \(abc1234\)/);
  assert.match(entry, /### Bug Fixes\n\n- stop the crash \(def5678\)/);
  assert.match(entry, /### Other Changes\n\n- tidy the readme \(aaa1111\)/);
});

test('renderEntry keeps breaking changes out of the type sections', () => {
  const commits = [{ type: 'feat', scope: '', breaking: true, description: 'drop v1', short: 'bbb2222' }];
  const entry = renderEntry('1.0.0', '2026-10-19', commits);
  assert.match(entry, /### Breaking Changes\n\n- drop v1 \(bbb2222\)/);
  assert.doesNotMatch(entry, /### Features/);
});

test('prependEntry seeds a new changelog with the header', () => {
  const entry = '## [0.13.1] - 2026-10-19\n\n### Bug Fixes\n\n- a fix (abc1234)\n';
  assert.equal(prependEntry('', entry), `# Changelog\n\n${entry}`);
});

test('prependEntry puts the newest entry first and keeps the header', () => {
  const older = '# Changelog\n\n## [0.13.0] - 2026-10-18\n\n### Features\n\n- old (0000000)\n';
  const entry = '## [0.13.1] - 2026-10-19\n\n### Bug Fixes\n\n- new (abc1234)\n';
  const result = prependEntry(older, entry);
  assert.ok(result.startsWith('# Changelog\n\n## [0.13.1]'));
  assert.ok(result.includes('## [0.13.0] - 2026-10-18'));
  assert.equal(result.match(/# Changelog/g).length, 1);
});

test('prependEntry adds a header when the existing file lacks one', () => {
  const result = prependEntry('## [0.13.0]\n\n- old\n', '## [0.13.1]\n\n- new\n');
  assert.ok(result.startsWith('# Changelog\n\n## [0.13.1]'));
  assert.ok(result.includes('## [0.13.0]'));
});

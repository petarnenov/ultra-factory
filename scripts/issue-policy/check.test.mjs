import test from 'node:test';
import assert from 'node:assert/strict';
import {
  AGENT_PIN_BRANCH,
  PRIORITY_LABELS,
  TYPE_LABELS,
  evaluatePullRequest,
  exemptionFor,
  extractIssueReferences,
} from './check.mjs';

function referenced(entries) {
  return new Map(Object.entries(entries).map(([number, value]) => [Number(number), value]));
}

function issue(...labels) {
  return { labels, isPullRequest: false };
}

function pullRequest(...labels) {
  return { labels, isPullRequest: true };
}

function check(number, references, issues) {
  return evaluatePullRequest({ number, references, issues: referenced(issues) });
}

test('extracts a closing keyword reference', () => {
  assert.deepEqual(extractIssueReferences('Closes #123.'), [123]);
  assert.deepEqual(extractIssueReferences('fixes #7, closes #8'), [7, 8]);
});

test('extracts a plain reference and removes duplicates', () => {
  assert.deepEqual(extractIssueReferences('Tracks #42 and #42 again, plus #43.'), [42, 43]);
});

test('reads the title and the body as one reference list', () => {
  assert.deepEqual(extractIssueReferences('feat: add thing (#268)', 'Closes #268.'), [268]);
});

test('ignores cross-repository references and bare headings', () => {
  assert.deepEqual(extractIssueReferences('acme/other#12\n## Summary\n# Release'), []);
});

test('ignores references inside fenced code blocks', () => {
  const body = 'Closes #12.\n\n```text\n#412 references no issue: add "Closes #123"\n```\n';
  assert.deepEqual(extractIssueReferences(body), [12]);
});

test('ignores references inside inline code as literal text', () => {
  assert.deepEqual(extractIssueReferences('Why: `Fixes #456`'), []);
});

test('ignores a quoted failure message and a placeholder', () => {
  const body = 'The check said `#412 references #123`; replace `Closes #123`.';
  assert.deepEqual(extractIssueReferences(body), []);
});

test('passes when the referenced issue has a type and a priority', () => {
  assert.deepEqual(check(412, [123], { 123: issue('bug', 'priority:high') }), []);
});

test('ignores unrelated backlog labels', () => {
  const labels = ['rw', 'co:task', 'status:ready', 'wave:2', 'host:linux', 'needs-human'];
  assert.deepEqual(
    check(412, [123], { 123: issue('chore', 'priority:low', ...labels) }),
    [],
  );
});

test('fails with the priority fix when the issue has only a type label', () => {
  assert.deepEqual(check(412, [123], { 123: issue('documentation') }), [
    '#412 references #123, which has no priority label: '
      + 'gh issue edit 123 --add-label priority:medium',
  ]);
});

test('fails with the type fix when the issue has only a priority label', () => {
  assert.deepEqual(check(412, [123], { 123: issue('priority:critical') }), [
    '#412 references #123, which has no type label: '
      + 'gh issue edit 123 --add-label enhancement',
  ]);
});

test('fails on both missing labels when the issue has neither', () => {
  assert.deepEqual(check(412, [123], { 123: issue('rw', 'wave:3') }), [
    '#412 references #123, which has no type label: '
      + 'gh issue edit 123 --add-label enhancement',
    '#412 references #123, which has no priority label: '
      + 'gh issue edit 123 --add-label priority:medium',
  ]);
});

test('fails when the description references no issue', () => {
  assert.deepEqual(check(412, [], {}), [
    '#412 references no issue: add "Closes #123" to the description',
  ]);
});

test('fails when the only reference is another pull request', () => {
  assert.deepEqual(check(412, [99], { 99: pullRequest('enhancement', 'priority:low') }), [
    '#412 references no issue: add "Closes #123" to the description',
  ]);
});

test('checks the issue when a pull request is referenced next to it', () => {
  assert.deepEqual(
    check(412, [99, 123], { 99: pullRequest(), 123: issue('enhancement', 'priority:medium') }),
    [],
  );
});

test('fails when a referenced issue does not exist', () => {
  assert.deepEqual(check(412, [999], { 999: null }), [
    '#412 references #999, which does not exist in this repository: '
      + 'fix the reference or open the issue first',
  ]);
});

test('fails when an issue carries more than one type label', () => {
  assert.deepEqual(check(412, [123], { 123: issue('bug', 'enhancement', 'priority:high') }), [
    '#412 references #123, which has 2 type labels (bug, enhancement): '
      + 'gh issue edit 123 --remove-label enhancement',
  ]);
});

test('fails when an issue carries more than one priority label', () => {
  assert.deepEqual(
    check(412, [123], { 123: issue('bug', 'priority:high', 'priority:medium') }),
    [
      '#412 references #123, which has 2 priority labels (priority:high, priority:medium): '
        + 'gh issue edit 123 --remove-label priority:medium',
    ],
  );
});

test('does not count an unknown priority label as the priority', () => {
  assert.deepEqual(check(412, [123], { 123: issue('bug', 'priority:urgent') }), [
    '#412 references #123, which has no priority label: '
      + 'gh issue edit 123 --add-label priority:medium',
  ]);
});

test('exempts a promotion pull request of dev into main', () => {
  const reason = exemptionFor({
    base: { ref: 'main' },
    head: { ref: 'dev' },
  });
  assert.match(reason, /promotion of dev into main/);
});

test('exempts the automated agent-pin pull request', () => {
  const reason = exemptionFor({
    base: { ref: 'main' },
    head: { ref: AGENT_PIN_BRANCH },
  });
  assert.match(reason, /automated agent-pin refresh/);
});

test('does not exempt an ordinary feature pull request into dev', () => {
  assert.equal(exemptionFor({
    base: { ref: 'dev' },
    head: { ref: 'feat/issue-policy' },
  }), null);
});

test('does not exempt a feature pull request into main', () => {
  assert.equal(exemptionFor({
    base: { ref: 'main' },
    head: { ref: 'feat/issue-policy' },
  }), null);
});

test('declares the documented label sets', () => {
  assert.deepEqual([...TYPE_LABELS].sort(), [
    'accessibility', 'bug', 'chore', 'documentation', 'enhancement',
  ]);
  assert.deepEqual([...PRIORITY_LABELS].sort(), [
    'priority:critical', 'priority:high', 'priority:low', 'priority:medium',
  ]);
});

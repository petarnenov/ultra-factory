import test from 'node:test';
import assert from 'node:assert/strict';
import {
	activeSeconds,
	clipTimes,
	closingReferences,
	formatEvidence,
	formatHours,
	parseTimestamp,
	readSession,
	sumBlocks,
	timesOnBranch,
	toBlocks,
} from './lib.mjs';

const CODEX = [
	'{"timestamp":"2026-10-12T15:55:06.541Z","ordinal":0,"type":"session_meta",' +
		'"payload":{"cwd":"/Users/dev/wt","git":{"branch":"ao/task"}}}',
	'{"timestamp":"2026-10-12T15:55:10.541Z","ordinal":1,"type":"event_msg"}',
	'{"timestamp":"2026-10-12T15:55:20.541Z","ordinal":2,"type":"response_item"}',
].join('\n');

const CLAUDE = [
	'{"type":"last-prompt","sessionId":"abc"}',
	'{"timestamp":"2026-10-12T15:55:06.541Z","type":"assistant",' +
		'"cwd":"/Users/dev/wt","gitBranch":"dev"}',
	'not json at all',
	'{"timestamp":"not-a-date","type":"assistant"}',
	'{"timestamp":"2026-10-12T16:00:00.000Z","type":"assistant",' +
		'"gitBranch":"feat/task","prNumber":42,"prRepository":"petarnenov/ultra-factory"}',
	'{"timestamp":"2026-10-12T16:05:00.000Z","type":"assistant",' +
		'"gitBranch":"feat/task","prNumber":42,"prRepository":"petarnenov/ultra-factory"}',
].join('\n');

test('parseTimestamp reads an ISO instant and rejects other input', () => {
	assert.equal(parseTimestamp('2026-10-12T15:55:06.541Z'), 1791820506.541);
	assert.equal(parseTimestamp('nope'), null);
	assert.equal(parseTimestamp(undefined), null);
});

test('readSession reads the Codex shape, its payload cwd, and its branch', () => {
	const session = readSession(CODEX);
	assert.equal(session.events.length, 3);
	assert.equal(session.cwd, '/Users/dev/wt');
	assert.equal(session.branch, 'ao/task');
	assert.equal(session.pull, null);
	assert.deepEqual(session.events.map((event) => event.at), [...session.events]
		.map((event) => event.at).sort((a, b) => a - b));
});

test('readSession reads the Claude shape and skips unusable lines', () => {
	const session = readSession(CLAUDE);
	assert.equal(session.events.length, 3);
	assert.equal(session.cwd, '/Users/dev/wt');
	assert.equal(session.branch, 'feat/task');
	assert.equal(session.pull, 42);
	assert.equal(session.pullRepository, 'petarnenov/ultra-factory');
});

test('readSession tolerates an empty log', () => {
	assert.deepEqual(readSession(''), {
		events: [],
		cwd: null,
		branch: null,
		pull: null,
		pullRepository: null,
	});
});

test('timesOnBranch keeps only the events recorded on that branch', () => {
	const { events } = readSession(CLAUDE);
	assert.deepEqual(timesOnBranch(events, 'feat/task'), [
		1791820800, 1791821100,
	]);
	assert.deepEqual(timesOnBranch(events, 'other'), []);
});

test('timesOnBranch keeps every event when the branch is recorded once', () => {
	const { events } = readSession(CODEX);
	assert.equal(timesOnBranch(events, 'ao/task').length, 3);
});

test('activeSeconds sums gaps and drops anything past the idle cut', () => {
	const times = [0, 60, 120, 1080];
	assert.equal(activeSeconds(times, 300), 60 + 60 + 300);
});

test('activeSeconds returns zero for a single event', () => {
	assert.equal(activeSeconds([5_000], 300), 0);
});

test('clipTimes keeps only the events up to the merge time', () => {
	assert.deepEqual(clipTimes([1, 2, 100], 50), [1, 2]);
	assert.deepEqual(clipTimes([1, 2], null), [1, 2]);
});

test('toBlocks converts seconds into 4-hour blocks', () => {
	assert.equal(toBlocks(4 * 60 * 60), 1);
	assert.equal(toBlocks(60 * 60), 0.25);
	assert.equal(toBlocks(0), 0);
});

test('sumBlocks adds and rounds to two decimals', () => {
	assert.equal(sumBlocks([0.5, 1.25]), 1.75);
	assert.equal(sumBlocks([1.005, 1.005]), 2.01);
	assert.equal(sumBlocks([]), 0);
});

test('formatHours rounds to two decimals', () => {
	assert.equal(formatHours(14_616), 4.06);
});

test('closingReferences reads closing keywords and ignores plain mentions', () => {
	assert.deepEqual(closingReferences('Closes #219.'), [219]);
	assert.deepEqual(closingReferences('Fixes: #12 and resolves #13'), [12, 13]);
	assert.deepEqual(closingReferences('see #412 in the example'), []);
	assert.deepEqual(closingReferences('Closes #5, closes #5'), [5]);
	assert.deepEqual(closingReferences('owner/repo#7'), []);
	assert.deepEqual(closingReferences('```\nCloses #1\n```'), []);
});

test('formatEvidence renders a table and a measured total', () => {
	const sessions = [
		{ name: 'claude/abc', times: [0, 3600], seconds: 3600 },
		{ name: 'codex/def', times: [0, 1800], seconds: 1800 },
	];
	const evidence = formatEvidence(sessions, {
		pullRequest: 282,
		mergedAt: '2026-10-18T10:00:00Z',
	});
	assert.match(evidence, /\| session \| active \| blocks \| span \|/);
	assert.match(evidence, /\| claude\/abc \| 1h \| 0.25 \|/);
	assert.match(evidence, /→ \*\*0.38 blocks\*\*/);
	assert.match(evidence, /pull request #282, clipped at merge/);
});

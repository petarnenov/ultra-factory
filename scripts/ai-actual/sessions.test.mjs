import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import {
	copilotSessions,
	defaultRoot,
	discoverSessions,
	jsonlSessions,
	openCodeSessions,
	parseStoreList,
	sqliteSessions,
	storePaths,
} from './sessions.mjs';

const ALL = { windowStart: 0, windowEnd: null };

function tempDir() {
	return mkdtempSync(join(tmpdir(), 'ai-actual-'));
}

function iso(at) {
	return new Date(at * 1000).toISOString();
}

const CODEX = [
	'{"timestamp":"2026-10-19T10:00:00.000Z","type":"session_meta",' +
		'"payload":{"cwd":"/repo/wt","git":{"branch":"feat/codex"}}}',
	'{"timestamp":"2026-10-19T10:04:00.000Z","type":"event_msg"}',
].join('\n');

const CLAUDE = [
	'{"timestamp":"2026-10-19T11:00:00.000Z","type":"assistant",' +
		'"cwd":"/repo/wt","gitBranch":"feat/claude"}',
	'{"timestamp":"2026-10-19T11:02:00.000Z","type":"assistant","gitBranch":"feat/claude"}',
].join('\n');

function jsonlFixture() {
	const root = tempDir();
	const codexRoot = join(root, 'codex');
	const claudeRoot = join(root, 'claude');
	mkdirSync(join(codexRoot, '2026/10/19'), { recursive: true });
	writeFileSync(
		join(codexRoot, '2026/10/19/rollout-2026-10-19T10-00-00-01a130db-c056-7480-9651-1f894b43c3c8.jsonl'),
		CODEX,
	);
	mkdirSync(join(claudeRoot, '-Users-me-repo'), { recursive: true });
	writeFileSync(join(claudeRoot, '-Users-me-repo', 'abcdefgh-1234.jsonl'), CLAUDE);
	return { codexRoot, claudeRoot };
}

test('jsonlSessions reads Codex and Claude fixtures with their metadata', () => {
	const { codexRoot, claudeRoot } = jsonlFixture();
	const codex = jsonlSessions({ name: 'codex', root: codexRoot, ...ALL });
	assert.equal(codex.length, 1);
	assert.equal(codex[0].name.startsWith('codex/'), true);
	assert.equal(codex[0].cwd, '/repo/wt');
	assert.equal(codex[0].branch, 'feat/codex');
	assert.equal(iso(codex[0].events[0].at), '2026-10-19T10:00:00.000Z');

	const claude = jsonlSessions({ name: 'claude', root: claudeRoot, ...ALL });
	assert.equal(claude.length, 1);
	assert.equal(claude[0].name.startsWith('claude/'), true);
	assert.equal(claude[0].branch, 'feat/claude');
	assert.equal(claude[0].events.length, 2);
});

test('jsonlSessions drops a session outside the window', () => {
	const { codexRoot } = jsonlFixture();
	const windowStart = Date.parse('2026-10-20T00:00:00Z') / 1000;
	assert.deepEqual(jsonlSessions({ name: 'codex', root: codexRoot, windowStart, windowEnd: null }), []);
});

test('jsonlSessions gives sessions that share an id prefix distinct labels', () => {
	const root = tempDir();
	writeFileSync(join(root, 'abcdefgh-1111.jsonl'), CLAUDE);
	writeFileSync(join(root, 'abcdefgh-2222.jsonl'), CLAUDE);
	const names = jsonlSessions({ name: 'claude', root, ...ALL })
		.map((session) => session.name)
		.sort();
	assert.deepEqual(names, ['claude/abcdefgh', 'claude/abcdefgh-2']);
});

test('jsonlSessions tolerates a missing root', () => {
	assert.deepEqual(jsonlSessions({ name: 'claude', root: join(tempDir(), 'nope'), ...ALL }), []);
});

function openCodeFixture() {
	const path = join(tempDir(), 'opencode.db');
	const database = new DatabaseSync(path);
	database.exec(`
		create table session_v2 (id text primary key, slug text, directory text);
		create table session_message (
			id text primary key, session_id text, time_created integer
		);
	`);
	const insertSession = database.prepare('insert into session_v2 values (?, ?, ?)');
	const insertMessage = database.prepare('insert into session_message values (?, ?, ?)');
	insertSession.run('ses_1', 'clever-panda', '/repo/wt');
	insertMessage.run('m1', 'ses_1', Date.parse('2026-10-19T10:20:00Z'));
	insertMessage.run('m2', 'ses_1', Date.parse('2026-10-19T10:25:00Z'));
	insertSession.run('ses_2', 'old-moon', '/repo/old');
	insertMessage.run('m3', 'ses_2', Date.parse('2023-11-14T22:00:00Z'));
	database.close();
	return path;
}

test('openCodeSessions reads the slug, directory, and millisecond times', () => {
	const sessions = openCodeSessions({ name: 'opencode', root: openCodeFixture(), ...ALL });
	assert.equal(sessions.length, 2);
	const current = sessions.find((session) => session.name === 'opencode/clever-panda');
	assert.equal(current.cwd, '/repo/wt');
	assert.equal(current.branch, null, 'OpenCode records no branch');
	assert.equal(current.events.length, 2);
	assert.equal(iso(current.events[0].at), '2026-10-19T10:20:00.000Z');
});

test('openCodeSessions drops a session outside the window', () => {
	const windowStart = Date.parse('2026-01-01T00:00:00Z') / 1000;
	const sessions = openCodeSessions({ name: 'opencode', root: openCodeFixture(), windowStart, windowEnd: null });
	assert.deepEqual(sessions.map((session) => session.name), ['opencode/clever-panda']);
});

test('openCodeSessions gives two sessions that share a slug distinct labels', () => {
	const path = join(tempDir(), 'opencode.db');
	const database = new DatabaseSync(path);
	database.exec(`
		create table session_v2 (id text primary key, slug text, directory text);
		create table session_message (
			id text primary key, session_id text, time_created integer
		);
	`);
	const insertSession = database.prepare('insert into session_v2 values (?, ?, ?)');
	const insertMessage = database.prepare('insert into session_message values (?, ?, ?)');
	insertSession.run('ses_a', 'clever-panda', '/repo/one');
	insertSession.run('ses_b', 'clever-panda', '/repo/two');
	insertMessage.run('m1', 'ses_a', Date.parse('2026-10-19T10:00:00Z'));
	insertMessage.run('m2', 'ses_b', Date.parse('2026-10-19T10:05:00Z'));
	database.close();

	const names = openCodeSessions({ name: 'opencode', root: path, ...ALL })
		.map((session) => session.name)
		.sort();
	assert.deepEqual(names, ['opencode/clever-panda', 'opencode/clever-panda-2']);
});

test('openCodeSessions does not reuse a label that already looks suffixed', () => {
	const path = join(tempDir(), 'opencode.db');
	const database = new DatabaseSync(path);
	database.exec(`
		create table session_v2 (id text primary key, slug text, directory text);
		create table session_message (
			id text primary key, session_id text, time_created integer
		);
	`);
	const insertSession = database.prepare('insert into session_v2 values (?, ?, ?)');
	const insertMessage = database.prepare('insert into session_message values (?, ?, ?)');
	for (const [id, slug] of [['ses_a', 'dup'], ['ses_b', 'dup'], ['ses_c', 'dup-2']]) {
		insertSession.run(id, slug, `/repo/${id}`);
		insertMessage.run(`m-${id}`, id, Date.parse('2026-10-19T10:00:00Z'));
	}
	database.close();

	const names = openCodeSessions({ name: 'opencode', root: path, ...ALL })
		.map((session) => session.name);
	assert.equal(names.length, 3);
	assert.equal(new Set(names).size, 3);
});

function copilotFixture(path = join(tempDir(), 'session-store.db'), { descending = false } = {}) {
	mkdirSync(dirname(path), { recursive: true });
	const database = new DatabaseSync(path);
	database.exec(`
		create table sessions (id text primary key, cwd text, branch text);
		create table turns (id integer primary key, session_id text, timestamp text);
	`);
	database.prepare('insert into sessions values (?, ?, ?)')
		.run('a1b2c3d4-0000-0000-0000-000000000000', '/repo/wt', 'feat/copilot');
	const insertTurn = database.prepare('insert into turns values (?, ?, ?)');
	// SQLite's datetime() writes a space and no zone, meaning UTC.
	const turns = [
		[1, 'a1b2c3d4-0000-0000-0000-000000000000', '2026-10-19 10:00:00'],
		[2, 'a1b2c3d4-0000-0000-0000-000000000000', '2026-10-19T10:30:00.000Z'],
	];
	if (descending) turns.reverse();
	for (const turn of turns) insertTurn.run(...turn);
	database.close();
	return path;
}

function homeWithCodex() {
	const home = tempDir();
	const root = join(home, '.codex', 'sessions', '2026/10/19');
	mkdirSync(root, { recursive: true });
	writeFileSync(join(root, 'rollout-2026-10-19T10-00-00-01a130db-c056-7480-9651-1f894b43c3c8.jsonl'), CODEX);
	return home;
}

test('copilotSessions reads the cwd, branch, and turn times as UTC', () => {
	const sessions = copilotSessions({ name: 'copilot', root: copilotFixture(), ...ALL });
	assert.equal(sessions.length, 1);
	const session = sessions[0];
	assert.equal(session.name, 'copilot/a1b2c3d4');
	assert.equal(session.cwd, '/repo/wt');
	assert.equal(session.branch, 'feat/copilot');
	assert.equal(iso(session.events[0].at), '2026-10-19T10:00:00.000Z');
	assert.equal(iso(session.events[1].at), '2026-10-19T10:30:00.000Z');
	// Every turn carries the session branch, so a branch match keeps them.
	assert.deepEqual(session.events.map((event) => event.branch), ['feat/copilot', 'feat/copilot']);
});

test('sqliteSessions tolerates a missing database', () => {
	const root = join(tempDir(), 'nope.db');
	assert.deepEqual(openCodeSessions({ name: 'opencode', root, ...ALL }), []);
	assert.deepEqual(copilotSessions({ name: 'copilot', root, ...ALL }), []);
	assert.deepEqual(sqliteSessions({ name: 'opencode', root, ...ALL }), []);
});

test('defaultRoot names the documented path for every store', () => {
	const home = '/home/test';
	assert.equal(defaultRoot('codex', home), join(home, '.codex', 'sessions'));
	assert.equal(defaultRoot('claude', home), join(home, '.claude', 'projects'));
	assert.equal(defaultRoot('opencode', home), join(home, '.local', 'share', 'opencode', 'opencode.db'));
	assert.equal(defaultRoot('copilot', home), join(home, '.copilot', 'session-store.db'));
	assert.throws(() => defaultRoot('unknown', home), /unknown store/);
});

test('storePaths tries the documented Copilot store before the snap store', () => {
	const home = '/home/test';
	assert.deepEqual(storePaths('copilot', { home, env: {} }), [
		join(home, '.copilot', 'session-store.db'),
		join(home, 'snap', 'copilot-cli', 'common', '.copilot', 'session-store.db'),
	]);
});

test('storePaths prefers a Copilot override from the flag or the environment', () => {
	const home = '/home/test';
	assert.deepEqual(storePaths('copilot', { home, copilotStore: '/tmp/flag.db', env: {} }), ['/tmp/flag.db']);
	assert.deepEqual(
		storePaths('copilot', { home, env: { AI_ACTUAL_COPILOT_STORE: '/tmp/env.db' } }),
		['/tmp/env.db'],
	);
	assert.deepEqual(
		storePaths('copilot', {
			home,
			copilotStore: '/tmp/flag.db',
			env: { AI_ACTUAL_COPILOT_STORE: '/tmp/env.db' },
		}),
		['/tmp/flag.db'],
	);
});

test('discoverSessions resolves both Copilot layouts', () => {
	const documented = tempDir();
	copilotFixture(join(documented, '.copilot', 'session-store.db'));
	const fromDocumented = discoverSessions({
		...ALL,
		stores: ['copilot'],
		home: documented,
		env: {},
		warn: () => assert.fail('unexpected warning'),
	});
	assert.equal(fromDocumented.length, 1);

	const snap = tempDir();
	copilotFixture(join(snap, 'snap', 'copilot-cli', 'common', '.copilot', 'session-store.db'));
	const fromSnap = discoverSessions({
		...ALL,
		stores: ['copilot'],
		home: snap,
		env: {},
		warn: () => assert.fail('unexpected warning'),
	});
	assert.equal(fromSnap.length, 1);
});

test('discoverSessions resolves a Copilot override from the flag or environment', () => {
	const byFlag = discoverSessions({
		...ALL,
		stores: ['copilot'],
		copilotStore: copilotFixture(),
		env: {},
		warn: () => assert.fail('unexpected warning'),
	});
	assert.equal(byFlag.length, 1);

	const byEnvironment = discoverSessions({
		...ALL,
		stores: ['copilot'],
		env: { AI_ACTUAL_COPILOT_STORE: copilotFixture() },
		warn: () => assert.fail('unexpected warning'),
	});
	assert.equal(byEnvironment.length, 1);
});

test('discoverSessions warns with the tool and every path when a store is missing', () => {
	const home = tempDir();
	const warnings = [];
	const sessions = discoverSessions({
		...ALL,
		stores: ['copilot'],
		home,
		env: {},
		warn: (line) => warnings.push(line),
	});
	assert.deepEqual(sessions, []);
	assert.equal(warnings.length, 1);
	assert.match(warnings[0], /copilot/);
	assert.ok(warnings[0].includes(join(home, '.copilot', 'session-store.db')));
	assert.ok(warnings[0].includes(join(home, 'snap', 'copilot-cli', 'common', '.copilot', 'session-store.db')));
});

test('discoverSessions keeps measuring other tools when a Copilot store is corrupt', () => {
	const home = homeWithCodex();
	const corrupt = join(home, '.copilot', 'session-store.db');
	mkdirSync(dirname(corrupt), { recursive: true });
	writeFileSync(corrupt, 'not a sqlite database');
	const warnings = [];
	const sessions = discoverSessions({
		...ALL,
		stores: ['copilot', 'codex'],
		home,
		env: {},
		warn: (line) => warnings.push(line),
	});
	assert.equal(sessions.filter((session) => session.name.startsWith('codex/')).length, 1);
	assert.equal(warnings.length, 1);
	assert.match(warnings[0], /copilot/);
	assert.ok(warnings[0].includes(corrupt));
});

test('copilotSessions sorts turns before applying the window', () => {
	const path = copilotFixture(undefined, { descending: true });
	const windowStart = Date.parse('2026-10-19T10:15:00Z') / 1000;
	const sessions = copilotSessions({ name: 'copilot', root: path, windowStart, windowEnd: null });
	assert.equal(sessions.length, 1);
	assert.equal(iso(sessions[0].events[0].at), '2026-10-19T10:00:00.000Z');
});

test('parseStoreList trims and deduplicates tools', () => {
	assert.deepEqual(parseStoreList(' codex, codex , claude '), ['codex', 'claude']);
	assert.deepEqual(parseStoreList('codex,,codex'), ['codex']);
	assert.equal(parseStoreList(undefined), undefined);
});

test('discoverSessions does not double count a duplicated store', () => {
	const sessions = discoverSessions({
		...ALL,
		stores: ['codex', 'codex'],
		home: homeWithCodex(),
		env: {},
		warn: () => assert.fail('unexpected warning'),
	});
	assert.equal(sessions.length, 1);
});

test('discoverSessions rejects an unknown store', () => {
	assert.throws(() => discoverSessions({ ...ALL, stores: ['unknown'] }), /unknown store/);
});

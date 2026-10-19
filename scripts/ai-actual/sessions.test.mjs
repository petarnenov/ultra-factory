import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import {
	copilotSessions,
	discoverSessions,
	jsonlSessions,
	openCodeSessions,
	sqliteSessions,
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

function copilotFixture() {
	const path = join(tempDir(), 'session-store.db');
	const database = new DatabaseSync(path);
	database.exec(`
		create table sessions (id text primary key, cwd text, branch text);
		create table turns (id integer primary key, session_id text, timestamp text);
	`);
	database.prepare('insert into sessions values (?, ?, ?)')
		.run('a1b2c3d4-0000-0000-0000-000000000000', '/repo/wt', 'feat/copilot');
	const insertTurn = database.prepare('insert into turns values (?, ?, ?)');
	// SQLite's datetime() writes a space and no zone, meaning UTC.
	insertTurn.run(1, 'a1b2c3d4-0000-0000-0000-000000000000', '2026-10-19 10:00:00');
	insertTurn.run(2, 'a1b2c3d4-0000-0000-0000-000000000000', '2026-10-19T10:30:00.000Z');
	database.close();
	return path;
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

test('discoverSessions rejects an unknown store', () => {
	assert.throws(() => discoverSessions({ ...ALL, stores: ['unknown'] }), /unknown store/);
});

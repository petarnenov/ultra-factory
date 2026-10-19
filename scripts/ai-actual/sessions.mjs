// Session stores for Codex, Claude Code, OpenCode, and the Copilot CLI.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { homedir } from 'node:os';
import { basename, join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { readSession, toEpochSeconds } from './lib.mjs';

// Codex and Claude Code write JSONL under a directory; OpenCode and the
// Copilot CLI keep SQLite databases.
export const STORES = {
	codex: { kind: 'jsonl' },
	claude: { kind: 'jsonl' },
	opencode: { kind: 'sqlite' },
	copilot: { kind: 'sqlite' },
};

const DAY_SECONDS = 86_400;

export function defaultRoot(name) {
	if (name === 'codex') return join(homedir(), '.codex', 'sessions');
	if (name === 'claude') return join(homedir(), '.claude', 'projects');
	if (name === 'opencode') return join(homedir(), '.local', 'share', 'opencode', 'opencode.db');
	if (name === 'copilot') return join(homedir(), '.copilot', 'session-store.db');
	throw new Error(`unknown store ${name}`);
}

export function discoverSessions({ windowStart, windowEnd, stores }) {
	const names = stores ?? Object.keys(STORES);
	const found = [];
	for (const name of names) {
		if (!Object.hasOwn(STORES, name)) throw new Error(`unknown store ${name}`);
		const root = defaultRoot(name);
		const options = { name, root, windowStart, windowEnd };
		found.push(...(STORES[name].kind === 'jsonl' ? jsonlSessions(options) : sqliteSessions(options)));
	}
	return found;
}

function inWindow(events, windowStart, windowEnd) {
	if (events.length === 0) return false;
	if (events[events.length - 1].at < windowStart) return false;
	if (windowEnd && events[0].at > windowEnd + DAY_SECONDS) return false;
	return true;
}

function session(name, cwd, branch, events) {
	events.sort((left, right) => left.at - right.at);
	return { name, cwd, branch, events, pull: null, pullRepository: null };
}

function walk(root) {
	const found = [];
	const stack = [root];
	while (stack.length > 0) {
		const dir = stack.pop();
		let entries;
		try {
			entries = readdirSync(dir, { withFileTypes: true });
		} catch {
			continue;
		}
		for (const entry of entries) {
			const path = join(dir, entry.name);
			if (entry.isDirectory()) stack.push(path);
			else if (entry.name.endsWith('.jsonl')) found.push(path);
		}
	}
	return found;
}

function label(name, path) {
	const file = basename(path, '.jsonl');
	if (name === 'codex' && file.startsWith('rollout-')) {
		return `codex/${file.split('-').pop().slice(0, 8)}`;
	}
	return `${name}/${file.slice(0, 8)}`;
}

export function jsonlSessions({ name, root, windowStart, windowEnd }) {
	const sessions = [];
	for (const path of walk(root)) {
		let mtime;
		try {
			mtime = statSync(path).mtimeMs / 1000;
		} catch {
			continue;
		}
		if (mtime < windowStart) continue;
		if (windowEnd && mtime > windowEnd + DAY_SECONDS) continue;
		const parsed = readSession(readFileSync(path, 'utf8'));
		if (!inWindow(parsed.events, windowStart, windowEnd)) continue;
		sessions.push({ ...session(label(name, path), parsed.cwd, parsed.branch, parsed.events),
			pull: parsed.pull, pullRepository: parsed.pullRepository });
	}
	return sessions;
}

function withDatabase(path, read) {
	try {
		statSync(path);
	} catch {
		return [];
	}
	const database = new DatabaseSync(path, { readOnly: true });
	try {
		return read(database);
	} finally {
		database.close();
	}
}

export function openCodeSessions({ name, root, windowStart, windowEnd }) {
	return withDatabase(root, (database) => {
		const rows = database.prepare(`
			select session_message.session_id as id,
				session_v2.slug as slug,
				session_v2.directory as directory,
				session_message.time_created as at
			from session_message
			join session_v2 on session_v2.id = session_message.session_id
		`).all();
		const grouped = new Map();
		for (const row of rows) {
			const key = String(row.id);
			if (!grouped.has(key)) {
				grouped.set(key, { slug: row.slug, cwd: row.directory, events: [] });
			}
			grouped.get(key).events.push({ at: toEpochSeconds(row.at), branch: null, pull: null });
		}
		const sessions = [];
		for (const [id, group] of grouped) {
			// OpenCode records no branch: the worktree has to name one.
			const events = group.events.filter((event) => event.at !== null);
			if (!inWindow(events, windowStart, windowEnd)) continue;
			sessions.push(session(`${name}/${group.slug ?? id.slice(0, 8)}`, group.cwd, null, events));
		}
		return sessions;
	});
}

export function copilotSessions({ name, root, windowStart, windowEnd }) {
	return withDatabase(root, (database) => {
		const sessions = database.prepare('select id, cwd, branch from sessions').all();
		const turns = database.prepare('select session_id, timestamp from turns').all();
		const grouped = new Map();
		for (const turn of turns) {
			const key = String(turn.session_id);
			if (!grouped.has(key)) grouped.set(key, []);
			grouped.get(key).push({ at: toEpochSeconds(turn.timestamp), branch: null, pull: null });
		}
		const found = [];
		for (const row of sessions) {
			const events = (grouped.get(String(row.id)) ?? []).filter((event) => event.at !== null);
			if (!inWindow(events, windowStart, windowEnd)) continue;
			// The store records one branch per session, so every turn on that
			// session carries it.
			for (const event of events) event.branch = row.branch;
			found.push(session(`${name}/${String(row.id).slice(0, 8)}`, row.cwd, row.branch, events));
		}
		return found;
	});
}

export function sqliteSessions(options) {
	if (options.name === 'opencode') return openCodeSessions(options);
	if (options.name === 'copilot') return copilotSessions(options);
	throw new Error(`no reader for ${options.name}`);
}

// Session stores for Codex, Claude Code, OpenCode, and the Copilot CLI.

import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
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
export const COPILOT_STORE_ENV = 'AI_ACTUAL_COPILOT_STORE';

export function defaultRoot(name, home = homedir()) {
	if (name === 'codex') return join(home, '.codex', 'sessions');
	if (name === 'claude') return join(home, '.claude', 'projects');
	if (name === 'opencode') return join(home, '.local', 'share', 'opencode', 'opencode.db');
	if (name === 'copilot') return join(home, '.copilot', 'session-store.db');
	throw new Error(`unknown store ${name}`);
}

// A Linux snap install of the Copilot CLI moves its store under ~/snap.
export function storePaths(name, { home = homedir(), copilotStore, env = process.env } = {}) {
	if (name !== 'copilot') return [defaultRoot(name, home)];
	const override = copilotStore || env[COPILOT_STORE_ENV];
	if (override) return [override];
	return [
		defaultRoot(name, home),
		join(home, 'snap', 'copilot-cli', 'common', '.copilot', 'session-store.db'),
	];
}

// Trims, drops empty entries, and keeps one of each store name.
export function parseStoreList(value) {
	if (value === undefined || value === null) return undefined;
	const names = String(value).split(',').map((name) => name.trim()).filter(Boolean);
	return [...new Set(names)];
}

export function discoverSessions({
	windowStart,
	windowEnd,
	stores,
	home = homedir(),
	copilotStore,
	env = process.env,
	warn = console.warn,
}) {
	const names = [...new Set(stores ?? Object.keys(STORES))];
	const found = [];
	for (const name of names) {
		if (!Object.hasOwn(STORES, name)) throw new Error(`unknown store ${name}`);
		const kind = STORES[name].kind;
		const paths = storePaths(name, { home, copilotStore, env });
		const failures = [];
		let read = false;
		for (const path of paths) {
			try {
				if (!existsSync(path)) throw new Error('not found');
				if (kind === 'jsonl') readdirSync(path);
				else statSync(path);
				const options = { name, root: path, windowStart, windowEnd };
				found.push(...(kind === 'jsonl' ? jsonlSessions(options) : sqliteSessions(options)));
				read = true;
				break;
			} catch (error) {
				// One unreadable store must not hide the other tools' time.
				failures.push(`${path} (${error.message})`);
			}
		}
		if (!read) {
			warn(`warning: ${name} session store unavailable; tried ${failures.join(', ')}; skipping ${name}`);
		}
	}
	return found;
}

// Callers pass events in time order; SQL readers sort before calling.
function inWindow(events, windowStart, windowEnd) {
	if (events.length === 0) return false;
	if (events[events.length - 1].at < windowStart) return false;
	if (windowEnd && events[0].at > windowEnd + DAY_SECONDS) return false;
	return true;
}

function byTime(left, right) {
	return left.at - right.at;
}

function session(name, cwd, branch, events) {
	events.sort(byTime);
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

// Two sessions can share a shortened id or an OpenCode slug, so a repeated
// label gets a numeric suffix until it no longer names another session.
function uniqueLabel(base, used) {
	if (!used.has(base)) {
		used.add(base);
		return base;
	}
	let count = 2;
	while (used.has(`${base}-${count}`)) count += 1;
	const label = `${base}-${count}`;
	used.add(label);
	return label;
}

export function jsonlSessions({ name, root, windowStart, windowEnd }) {
	const sessions = [];
	const used = new Set();
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
		sessions.push({ ...session(uniqueLabel(label(name, path), used), parsed.cwd,
			parsed.branch, parsed.events), pull: parsed.pull, pullRepository: parsed.pullRepository });
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
		const used = new Set();
		for (const [id, group] of grouped) {
			// OpenCode records no branch: the worktree has to name one.
			const events = group.events.filter((event) => event.at !== null).sort(byTime);
			if (!inWindow(events, windowStart, windowEnd)) continue;
			const label = uniqueLabel(`${name}/${group.slug ?? id.slice(0, 8)}`, used);
			sessions.push(session(label, group.cwd, null, events));
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
		const used = new Set();
		for (const row of sessions) {
			const events = (grouped.get(String(row.id)) ?? [])
				.filter((event) => event.at !== null)
				.sort(byTime);
			if (!inWindow(events, windowStart, windowEnd)) continue;
			// The store records one branch per session, so every turn on that
			// session carries it.
			for (const event of events) event.branch = row.branch;
			const label = uniqueLabel(`${name}/${String(row.id).slice(0, 8)}`, used);
			found.push(session(label, row.cwd, row.branch, events));
		}
		return found;
	});
}

export function sqliteSessions(options) {
	if (options.name === 'opencode') return openCodeSessions(options);
	if (options.name === 'copilot') return copilotSessions(options);
	throw new Error(`no reader for ${options.name}`);
}

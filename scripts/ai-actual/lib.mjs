// Pure helpers for measuring coding-agent time from local session logs.

import { prosaicText } from '../issue-policy/check.mjs';

export const DEFAULT_IDLE_CUT_SECONDS = 5 * 60;
export const BLOCK_SECONDS = 4 * 60 * 60;
export const MIN_SESSION_SECONDS = 60;
export const ACTUAL_FIELD = 'Actual (ai4h)';

// GitHub's closing keywords, as listed in the pull request documentation.
const CLOSING_KEYWORDS = /(?<![\w/#])(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s*:?\s+#(\d+)\b/gi;

// Reads the issues a pull request closes. closingIssuesReferences is empty
// once the pull request is merged, so the body is parsed as well.
export function closingReferences(...texts) {
	const numbers = [];
	for (const text of texts) {
		for (const match of prosaicText(text).matchAll(CLOSING_KEYWORDS)) {
			const number = Number(match[1]);
			if (!numbers.includes(number)) numbers.push(number);
		}
	}
	return numbers;
}

export function round2(value) {
	return Math.round(value * 100) / 100;
}

export function parseTimestamp(value) {
	if (typeof value !== 'string') return null;
	const ms = Date.parse(value);
	return Number.isNaN(ms) ? null : ms / 1000;
}

// Reads a store timestamp: epoch milliseconds, epoch seconds, or a string
// that may need a UTC zone, as SQLite's datetime() writes.
export function toEpochSeconds(value) {
	if (typeof value === 'number') return value > 1e11 ? value / 1000 : value;
	if (typeof value !== 'string' || value === '') return null;
	const iso = value.includes('T') ? value : value.replace(' ', 'T');
	const zoned = /[Zz]|[+-]\d{2}:?\d{2}$/.test(iso) ? iso : `${iso}Z`;
	return parseTimestamp(zoned);
}

// Most frequent non-null value; ties keep the first one seen.
function mode(values) {
	const counts = new Map();
	for (const value of values) {
		if (value === null || value === undefined) continue;
		counts.set(value, (counts.get(value) ?? 0) + 1);
	}
	let best = null;
	let bestCount = 0;
	for (const [value, count] of counts) {
		if (count > bestCount) {
			best = value;
			bestCount = count;
		}
	}
	return best;
}

// Reads one JSONL session log into timestamped events plus the facts the
// log carries: working directory, branch, and linked pull request.
export function readSession(text) {
	const events = [];
	let cwd = null;
	let pullRepository = null;
	for (const line of text.split('\n')) {
		if (!line.includes('"timestamp"')) continue;
		let entry;
		try {
			entry = JSON.parse(line);
		} catch {
			continue;
		}
		const at = parseTimestamp(entry.timestamp);
		if (at === null) continue;
		events.push({
			at,
			branch: entry.gitBranch ?? entry.payload?.git?.branch ?? null,
			pull: entry.prNumber ?? null,
		});
		cwd = entry.cwd ?? entry.payload?.cwd ?? cwd;
		pullRepository = entry.prRepository ?? pullRepository;
	}
	events.sort((left, right) => left.at - right.at);
	return {
		events,
		cwd,
		branch: mode(events.map((event) => event.branch)),
		pull: mode(events.map((event) => event.pull)),
		pullRepository,
	};
}

// Times of the events recorded on a branch. A log that records the branch
// once, as Codex does in session_meta, keeps every event.
export function timesOnBranch(events, branch) {
	const on = events.filter((event) => event.branch === branch).map((event) => event.at);
	if (on.length > 1) return on;
	const first = events.find((event) => event.branch !== null);
	// A store that records the branch per session, not per event, applies it
	// to every event.
	if (!first) return events.map((event) => event.at);
	if (first.branch === branch) return events.map((event) => event.at);
	return on;
}

// Sums the gaps between events, dropping any gap past the idle cut. A gap
// longer than the cut is time the tool was not working on the task.
export function activeSeconds(times, idleCut = DEFAULT_IDLE_CUT_SECONDS) {
	let total = 0;
	for (let i = 1; i < times.length; i += 1) {
		total += Math.min(times[i] - times[i - 1], idleCut);
	}
	return total;
}

export function clipTimes(times, until) {
	if (!until) return times;
	return times.filter((value) => value <= until);
}

export function toBlocks(seconds, blockSeconds = BLOCK_SECONDS) {
	return round2(seconds / blockSeconds);
}

export function sumBlocks(values) {
	return round2(values.reduce((total, value) => total + value, 0));
}

export function formatHours(seconds) {
	return round2(seconds / 3600);
}

export function formatSpan(session) {
	const first = new Date(session.times[0] * 1000).toISOString();
	const last = new Date(session.times[session.times.length - 1] * 1000).toISOString();
	return `${first} .. ${last}`;
}

// Renders the markdown evidence block that is posted on the issue.
export function formatEvidence(sessions, options = {}) {
	const lines = ['| session | active | blocks | span |', '|---|---:|---:|---|'];
	for (const session of sessions) {
		lines.push(
			`| ${session.name} | ${formatHours(session.seconds)}h | ` +
				`${toBlocks(session.seconds)} | ${formatSpan(session)} |`,
		);
	}
	const seconds = sessions.reduce((total, session) => total + session.seconds, 0);
	lines.push('');
	const clip = options.mergedAt ? `, clipped at merge ${options.mergedAt}` : '';
	const source = options.pullRequest ? `pull request #${options.pullRequest}${clip}` : '';
	lines.push(
		`**Measured:** ${formatHours(seconds)}h of active tool time from ` +
			`${sessions.length} session(s) → **${toBlocks(seconds)} blocks**${source ? ` (${source})` : ''}.`,
	);
	return lines.join('\n');
}

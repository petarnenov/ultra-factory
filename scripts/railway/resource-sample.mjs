#!/usr/bin/env node
// Samples one AO host's memory route and optional processes as JSONL.

import { execFile } from 'node:child_process';
import { createWriteStream } from 'node:fs';
import { hostname as osHostname } from 'node:os';
import { pathToFileURL } from 'node:url';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);

export const MEMORY_ROUTE = '/api/v1/usage/sessions/memory';

const PS_FORMAT = 'pid=,ppid=,pcpu=,rss=,args=';

export function parseArgs(argv) {
	const opts = {
		label: '',
		baseUrl: 'http://127.0.0.1:3001',
		intervalSeconds: 60,
		durationSeconds: 3600,
		out: '-',
		ps: [],
	};
	for (let i = 0; i < argv.length; i += 1) {
		const flag = argv[i];
		const take = () => {
			const value = argv[i + 1];
			if (value === undefined || value.startsWith('--')) {
				throw new Error(`${flag} needs a value`);
			}
			i += 1;
			return value;
		};
		switch (flag) {
			case '--label':
				opts.label = take();
				break;
			case '--base-url':
				opts.baseUrl = take();
				break;
			case '--interval':
				opts.intervalSeconds = Number(take());
				break;
			case '--duration':
				opts.durationSeconds = Number(take());
				break;
			case '--out':
				opts.out = take();
				break;
			case '--ps':
				opts.ps.push(take());
				break;
			default:
				throw new Error(`unknown argument: ${flag}`);
		}
	}
	if (opts.label.trim() === '') {
		throw new Error('--label is required');
	}
	if (!Number.isInteger(opts.intervalSeconds) || opts.intervalSeconds < 1) {
		throw new Error('--interval must be a positive whole number of seconds');
	}
	if (!Number.isInteger(opts.durationSeconds) || opts.durationSeconds < 1) {
		throw new Error('--duration must be a positive whole number of seconds');
	}
	const url = new URL(opts.baseUrl);
	if (url.protocol !== 'http:' && url.protocol !== 'https:') {
		throw new Error(`--base-url must be http or https: ${opts.baseUrl}`);
	}
	for (const pattern of opts.ps) {
		new RegExp(pattern);
	}
	return opts;
}

export function buildMemoryUrl(baseUrl) {
	const url = new URL(baseUrl);
	url.pathname = `${url.pathname.replace(/\/+$/, '')}${MEMORY_ROUTE}`;
	return url.toString();
}

// nextTick returns the next epoch boundary for the sampling interval, so hosts
// started within the same minute sample at the same second.
export function nextTick(nowMs, intervalMs) {
	return Math.ceil(nowMs / intervalMs) * intervalMs;
}

export function parsePs(text, patterns = []) {
	const filters = patterns.map((pattern) => new RegExp(pattern));
	const rows = [];
	for (const line of text.split('\n')) {
		const match = /^\s*(\d+)\s+(\d+)\s+([\d.]+)\s+(\d+)\s+(.*\S)\s*$/.exec(line);
		if (!match) {
			continue;
		}
		const row = {
			pid: Number(match[1]),
			ppid: Number(match[2]),
			cpuPercent: Number(match[3]),
			rssKb: Number(match[4]),
			command: match[5],
		};
		if (filters.length > 0 && !filters.some((filter) => filter.test(row.command))) {
			continue;
		}
		rows.push(row);
	}
	return rows;
}

export async function readProcesses(patterns = []) {
	const { stdout } = await execFileAsync('ps', ['-eo', PS_FORMAT], {
		maxBuffer: 8 * 1024 * 1024,
	});
	return parsePs(stdout, patterns);
}

export async function fetchUsage(fetchImpl, baseUrl, timeoutMs = 10000) {
	const response = await fetchImpl(buildMemoryUrl(baseUrl), {
		headers: { accept: 'application/json' },
		signal: AbortSignal.timeout(timeoutMs),
	});
	if (!response.ok) {
		throw new Error(`usage route returned ${response.status}`);
	}
	return response.json();
}

export async function captureSample(opts, deps = {}) {
	const now = deps.now ?? Date.now;
	const capturedAt = new Date(now()).toISOString();
	const usage = await fetchUsage(deps.fetchImpl ?? fetch, opts.baseUrl, deps.timeoutMs);
	let processes = [];
	let processError;
	if (opts.ps.length > 0) {
		try {
			processes = await (deps.psImpl ? deps.psImpl(opts.ps) : readProcesses(opts.ps));
		} catch (err) {
			processError = err instanceof Error ? err.message : String(err);
		}
	}
	const record = { type: 'sample', label: opts.label, capturedAt, usage, processes };
	if (processError) {
		record.processError = processError;
	}
	return record;
}

// runCapture samples until the window has elapsed, writing one JSONL record
// per event: header, sample or error, then footer.
export async function runCapture(opts, deps = {}, write = () => {}) {
	const now = deps.now ?? Date.now;
	const sleep = deps.sleep ?? ((ms) => new Promise((resolve) => setTimeout(resolve, ms)));
	const readHostname = deps.hostname ?? osHostname;
	const intervalMs = opts.intervalSeconds * 1000;
	const durationMs = opts.durationSeconds * 1000;
	const startedAt = new Date(now()).toISOString();
	write({
		type: 'header',
		label: opts.label,
		hostname: readHostname(),
		baseUrl: opts.baseUrl,
		intervalSeconds: opts.intervalSeconds,
		durationSeconds: opts.durationSeconds,
		psPatterns: opts.ps,
		startedAt,
	});
	let next = nextTick(now(), intervalMs);
	let firstSampleAt = null;
	let samples = 0;
	let errors = 0;
	for (;;) {
		const wait = next - now();
		if (wait > 0) {
			await sleep(wait);
		}
		const nowMs = now();
		if (firstSampleAt !== null && nowMs - firstSampleAt >= durationMs) {
			break;
		}
		try {
			write(await captureSample(opts, deps));
			samples += 1;
		} catch (err) {
			errors += 1;
			write({
				type: 'error',
				label: opts.label,
				capturedAt: new Date(nowMs).toISOString(),
				message: err instanceof Error ? err.message : String(err),
			});
		}
		if (firstSampleAt === null) {
			firstSampleAt = nowMs;
		}
		next += intervalMs;
		if (next <= now()) {
			next = nextTick(now(), intervalMs);
		}
	}
	write({
		type: 'footer',
		label: opts.label,
		startedAt,
		finishedAt: new Date(now()).toISOString(),
		samples,
		errors,
	});
	return samples > 0 ? 0 : 1;
}

const isMain = Boolean(process.argv[1])
	&& import.meta.url === pathToFileURL(process.argv[1]).href;

if (isMain) {
	try {
		const opts = parseArgs(process.argv.slice(2));
		const stream = opts.out === '-'
			? process.stdout
			: createWriteStream(opts.out, { flags: 'a', mode: 0o600 });
		const write = (record) => stream.write(`${JSON.stringify(record)}\n`);
		const code = await runCapture(
			opts,
			{ fetchImpl: fetch, psImpl: readProcesses },
			write,
		);
		if (stream !== process.stdout) {
			await new Promise((resolve) => stream.end(resolve));
		}
		process.exitCode = code;
	} catch (err) {
		console.error(`resource-sample: ${err.message}`);
		console.error(
			'usage: resource-sample.mjs --label <name> [--base-url <url>] '
			+ '[--interval 60] [--duration 3600] [--ps <regex>] [--out <file>]',
		);
		process.exitCode = 2;
	}
}

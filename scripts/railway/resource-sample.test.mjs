import test from 'node:test';
import assert from 'node:assert/strict';
import {
	buildMemoryUrl,
	captureSample,
	nextTick,
	parseArgs,
	parsePs,
	runCapture,
} from './resource-sample.mjs';

test('parseArgs fills defaults from a label only', () => {
	const opts = parseArgs(['--label', 'hub']);
	assert.equal(opts.label, 'hub');
	assert.equal(opts.baseUrl, 'http://127.0.0.1:3001');
	assert.equal(opts.intervalSeconds, 60);
	assert.equal(opts.durationSeconds, 3600);
	assert.equal(opts.out, '-');
	assert.deepEqual(opts.ps, []);
});

test('parseArgs reads every override and repeats --ps', () => {
	const opts = parseArgs([
		'--label', 'z600',
		'--base-url', 'http://127.0.0.1:4100',
		'--interval', '30',
		'--duration', '120',
		'--out', '/tmp/z600.jsonl',
		'--ps', 'claude|codex',
		'--ps', 'tmux',
	]);
	assert.equal(opts.baseUrl, 'http://127.0.0.1:4100');
	assert.equal(opts.intervalSeconds, 30);
	assert.equal(opts.durationSeconds, 120);
	assert.equal(opts.out, '/tmp/z600.jsonl');
	assert.deepEqual(opts.ps, ['claude|codex', 'tmux']);
});

test('parseArgs rejects missing and invalid values', () => {
	assert.throws(() => parseArgs([]), /--label is required/);
	assert.throws(() => parseArgs(['--label']), /--label needs a value/);
	assert.throws(() => parseArgs(['--label', 'hub', '--nope']), /unknown argument/);
	assert.throws(
		() => parseArgs(['--label', 'hub', '--interval', '0']),
		/--interval must be a positive whole number/,
	);
	assert.throws(
		() => parseArgs(['--label', 'hub', '--duration', '1.5']),
		/--duration must be a positive whole number/,
	);
	assert.throws(
		() => parseArgs(['--label', 'hub', '--base-url', 'file:///tmp/x']),
		/--base-url must be http or https/,
	);
	assert.throws(
		() => parseArgs(['--label', 'hub', '--ps', '([']),
		/Invalid regular expression/,
	);
});

test('buildMemoryUrl appends the route without doubling slashes', () => {
	assert.equal(
		buildMemoryUrl('http://127.0.0.1:3001'),
		'http://127.0.0.1:3001/api/v1/usage/sessions/memory',
	);
	assert.equal(
		buildMemoryUrl('http://127.0.0.1:3001/'),
		'http://127.0.0.1:3001/api/v1/usage/sessions/memory',
	);
	assert.equal(
		buildMemoryUrl('https://host.example/ao'),
		'https://host.example/ao/api/v1/usage/sessions/memory',
	);
});

test('nextTick rounds up to the next interval boundary', () => {
	assert.equal(nextTick(0, 60000), 0);
	assert.equal(nextTick(1, 60000), 60000);
	assert.equal(nextTick(60000, 60000), 60000);
	assert.equal(nextTick(60001, 60000), 120000);
});

test('parsePs reads RSS and CPU rows and filters on the command', () => {
	const text = [
		'    1     0  0.0  1234 /usr/bin/tini -s -- ao',
		'   42     1  1.5  5678 node /opt/ao/hub/bff/src/main.ts',
		'   43    42  2.5 9012 /opt/ao/npm/bin/claude --resume',
		'bad line',
		'',
	].join('\n');
	const all = parsePs(text);
	assert.equal(all.length, 3);
	assert.deepEqual(all[1], {
		pid: 42,
		ppid: 1,
		cpuPercent: 1.5,
		rssKb: 5678,
		command: 'node /opt/ao/hub/bff/src/main.ts',
	});
	const filtered = parsePs(text, ['bff/src/main.ts|claude']);
	assert.deepEqual(filtered.map((row) => row.pid), [42, 43]);
});

test('captureSample records the route payload and filtered processes', async () => {
	const payload = { sessions: [{ sessionId: 's1', rssBytes: 10 }] };
	const record = await captureSample(
		{ label: 'hub', baseUrl: 'http://127.0.0.1:3001', ps: ['bff'] },
		{
			now: () => Date.parse('2026-10-19T12:00:00Z'),
			fetchImpl: async () => ({ ok: true, json: async () => payload }),
			psImpl: async (patterns) => [{ pid: 42, command: 'bff', patterns }],
		},
	);
	assert.equal(record.type, 'sample');
	assert.equal(record.capturedAt, '2026-10-19T12:00:00.000Z');
	assert.deepEqual(record.usage, payload);
	assert.deepEqual(record.processes, [
		{ pid: 42, command: 'bff', patterns: ['bff'] },
	]);
	assert.equal(record.processError, undefined);
});

test('captureSample keeps the usage reading when ps fails', async () => {
	const record = await captureSample(
		{ label: 'hub', baseUrl: 'http://127.0.0.1:3001', ps: ['bff'] },
		{
			fetchImpl: async () => ({ ok: true, json: async () => ({ sessions: [] }) }),
			psImpl: async () => {
				throw new Error('ps not found');
			},
		},
	);
	assert.deepEqual(record.processes, []);
	assert.equal(record.processError, 'ps not found');
});

test('captureSample rejects a non-OK route response', async () => {
	await assert.rejects(
		captureSample(
			{ label: 'hub', baseUrl: 'http://127.0.0.1:3001', ps: [] },
			{ fetchImpl: async () => ({ ok: false, status: 501 }) },
		),
		/usage route returned 501/,
	);
});

test('runCapture writes header, aligned samples and footer', async () => {
	let clock = Date.parse('2026-10-19T12:00:30Z');
	const records = [];
	const code = await runCapture(
		{
			label: 'hub',
			baseUrl: 'http://127.0.0.1:3001',
			intervalSeconds: 60,
			durationSeconds: 180,
			out: '-',
			ps: [],
		},
		{
			now: () => clock,
			sleep: async (ms) => {
				clock += ms;
			},
			hostname: () => 'hub-host',
			fetchImpl: async () => ({ ok: true, json: async () => ({ sessions: [] }) }),
			psImpl: async () => [],
		},
		(record) => records.push(record),
	);
	assert.equal(code, 0);
	assert.equal(records[0].type, 'header');
	assert.equal(records[0].hostname, 'hub-host');
	assert.equal(records[0].startedAt, '2026-10-19T12:00:30.000Z');
	const samples = records.filter((record) => record.type === 'sample');
	assert.equal(samples.length, 3);
	assert.deepEqual(
		samples.map((sample) => sample.capturedAt),
		[
			'2026-10-19T12:01:00.000Z',
			'2026-10-19T12:02:00.000Z',
			'2026-10-19T12:03:00.000Z',
		],
	);
	const footer = records.at(-1);
	assert.equal(footer.type, 'footer');
	assert.equal(footer.samples, 3);
	assert.equal(footer.errors, 0);
});

test('runCapture records errors and fails when no sample succeeds', async () => {
	let clock = 0;
	const records = [];
	const code = await runCapture(
		{
			label: 'z600',
			baseUrl: 'http://127.0.0.1:3001',
			intervalSeconds: 60,
			durationSeconds: 120,
			out: '-',
			ps: [],
		},
		{
			now: () => clock,
			sleep: async (ms) => {
				clock += ms;
			},
			fetchImpl: async () => {
				throw new Error('daemon unreachable');
			},
		},
		(record) => records.push(record),
	);
	assert.equal(code, 1);
	assert.equal(records.filter((record) => record.type === 'error').length, 2);
	const footer = records.at(-1);
	assert.equal(footer.type, 'footer');
	assert.equal(footer.samples, 0);
	assert.equal(footer.errors, 2);
});

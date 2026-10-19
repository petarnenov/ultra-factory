#!/usr/bin/env node
// Measures AI tool time for a pull request and dispatches the recorder.

import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { homedir } from 'node:os';
import { basename, join } from 'node:path';
import {
	activeSeconds,
	clipTimes,
	closingReferences,
	DEFAULT_IDLE_CUT_SECONDS,
	formatEvidence,
	formatHours,
	MIN_SESSION_SECONDS,
	readSession,
	sumBlocks,
	timesOnBranch,
	toBlocks,
} from './lib.mjs';

const USAGE = `Usage: npm run ai-actual -- --pr <number> [options]

Measures the active AI tool time of the sessions that worked on a pull
request and dispatches .github/workflows/ai-actual.yml to record it.

Options:
  --pr <number>        Pull request to measure (required)
  --issue <number>     Record this issue instead of the ones the PR closes
  --cwd <path>         Extra working directory to attribute sessions from
  --idle-cut <minutes> Gap treated as idle (default 5)
  --ref <branch>       Branch holding the workflow (default: default branch)
  --workflow <name>    Workflow file name (default ai-actual.yml)
  --dry-run            Print the measurement without dispatching
  --verbose            Print every candidate session and why it was skipped
  --help               Print this text`;

function parseArgs(argv) {
	const args = { idleCutMinutes: DEFAULT_IDLE_CUT_SECONDS / 60, dryRun: false, verbose: false };
	for (let i = 0; i < argv.length; i += 1) {
		const flag = argv[i];
		if (flag === '--help' || flag === '-h') args.help = true;
		else if (flag === '--dry-run') args.dryRun = true;
		else if (flag === '--verbose') args.verbose = true;
		else if (flag.startsWith('--')) {
			const key = flag.slice(2).replace(/-([a-z])/g, (_, c) => c.toUpperCase());
			i += 1;
			if (i >= argv.length) throw new Error(`${flag} needs a value`);
			args[key] = argv[i];
		} else throw new Error(`unexpected argument ${flag}`);
	}
	return args;
}

function gh(args) {
	return execFileSync('gh', args, { encoding: 'utf8' }).trim();
}

function ghJson(args) {
	return JSON.parse(gh(args));
}

function gitBranch(cwd) {
	try {
		return execFileSync('git', ['-C', cwd, 'rev-parse', '--abbrev-ref', 'HEAD'], {
			encoding: 'utf8',
			stdio: ['ignore', 'pipe', 'ignore'],
		}).trim();
	} catch {
		return null;
	}
}

function walkJsonl(root) {
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

function claudeSessionFiles() {
	const root = join(homedir(), '.claude', 'projects');
	let projects;
	try {
		projects = readdirSync(root, { withFileTypes: true });
	} catch {
		return [];
	}
	return projects
		.filter((entry) => entry.isDirectory())
		.flatMap((entry) => walkJsonl(join(root, entry.name)));
}

// Labels a session file the way it is printed in the evidence table.
function label(path) {
	const name = basename(path, '.jsonl');
	if (name.startsWith('rollout-')) {
		const id = name.split('-').pop();
		return `codex/${id.slice(0, 8)}`;
	}
	return `claude/${name.slice(0, 8)}`;
}

function candidates({ windowStart, windowEnd, verbose }) {
	const files = [
		...walkJsonl(join(homedir(), '.codex', 'sessions')),
		...claudeSessionFiles(),
	];
	const sessions = [];
	for (const path of files) {
		let mtime;
		try {
			mtime = statSync(path).mtimeMs / 1000;
		} catch {
			continue;
		}
		if (mtime < windowStart) continue;
		if (windowEnd && mtime > windowEnd + 86_400) continue;
		const session = readSession(readFileSync(path, 'utf8'));
		if (session.events.length === 0) continue;
		sessions.push({ path, name: label(path), ...session });
		if (verbose) {
			console.log(
				`candidate ${label(path)} cwd=${session.cwd ?? '?'} ` +
					`branch=${session.branch ?? '?'} events=${session.events.length}`,
			);
		}
	}
	return sessions;
}

function allTimes(session) {
	return session.events.map((event) => event.at);
}

// A session belongs to the pull request when the log names its branch or
// the pull request, or when its worktree still sits on the head branch.
function attribute({ sessions, headRefName, pull, repo, extraCwd }) {
	const matched = [];
	const unresolved = [];
	for (const session of sessions) {
		if (session.branch === headRefName) {
			matched.push({
				...session,
				times: timesOnBranch(session.events, headRefName),
				reason: `log branch ${headRefName}`,
			});
		} else if (session.pull === pull && session.pullRepository === repo) {
			matched.push({ ...session, times: allTimes(session), reason: `log pull #${pull}` });
		} else if (session.cwd && gitBranch(session.cwd) === headRefName) {
			matched.push({ ...session, times: allTimes(session), reason: `worktree ${headRefName}` });
		} else if (extraCwd && session.cwd?.startsWith(extraCwd)) {
			matched.push({ ...session, times: allTimes(session), reason: `cwd ${extraCwd}` });
		} else {
			unresolved.push(session);
		}
	}
	return { matched, unresolved };
}

function main() {
	const args = parseArgs(process.argv.slice(2));
	if (args.help) {
		console.log(USAGE);
		return;
	}
	if (!args.pr) throw new Error('--pr is required (see --help)');

	const repo = args.repo ?? ghJson(['repo', 'view', '--json', 'nameWithOwner']).nameWithOwner;
	const pr = ghJson([
		'pr', 'view', String(args.pr), '--repo', repo,
		'--json', 'number,url,title,body,headRefName,mergedAt,state,commits,closingIssuesReferences',
	]);
	const issues = args.issue
		? [Number(args.issue)]
		: [
			...(pr.closingIssuesReferences ?? []).map((issue) => issue.number),
			...closingReferences(pr.title, pr.body),
		].filter((number, index, all) => all.indexOf(number) === index);
	if (issues.length === 0) {
		throw new Error(`pull request #${pr.number} closes no issue; pass --issue <number>`);
	}

	const authored = (pr.commits ?? [])
		.map((commit) => Date.parse(commit.authoredDate) / 1000)
		.filter((value) => !Number.isNaN(value));
	const mergedAt = pr.mergedAt ? Date.parse(pr.mergedAt) / 1000 : null;
	const windowStart = authored.length > 0
		? Math.min(...authored) - 2 * 86_400
		: Date.now() / 1000 - 30 * 86_400;

	const found = candidates({ windowStart, windowEnd: mergedAt, verbose: args.verbose });
	const { matched, unresolved } = attribute({
		sessions: found,
		headRefName: pr.headRefName,
		pull: pr.number,
		repo,
		extraCwd: args.cwd,
	});

	const idleCut = Number(args.idleCutMinutes) * 60;
	const measuredSessions = matched
		.map((session) => {
			const times = clipTimes(session.times, mergedAt);
			return { name: session.name, times, seconds: activeSeconds(times, idleCut), reason: session.reason };
		})
		.filter((session) => session.times.length > 1);
	// A session that barely ran is noise in the evidence table, not time.
	const measured = measuredSessions.filter((session) => session.seconds >= MIN_SESSION_SECONDS);
	const dropped = measuredSessions.length - measured.length;

	if (measured.length === 0) {
		console.error(
			`no session recorded branch ${pr.headRefName} or pull request #${pr.number} ` +
				`(${found.length} candidate log files)`,
		);
		if (dropped > 0) console.error(`${dropped} session(s) measured under one minute`);
		for (const session of unresolved) {
			console.error(`  skipped ${session.name} cwd=${session.cwd ?? '?'} branch=${session.branch ?? '?'}`);
		}
		console.error('pass --cwd <worktree path> to attribute those sessions by directory');
		process.exitCode = 1;
		return;
	}

	const seconds = measured.reduce((total, session) => total + session.seconds, 0);
	for (const session of measured) {
		console.log(
			`${session.name}\t${formatHours(session.seconds)}h\t${toBlocks(session.seconds)} blocks\t${session.reason}`,
		);
	}
	const blocks = sumBlocks(measured.map((session) => toBlocks(session.seconds)));
	const evidence = formatEvidence(measured, { pullRequest: pr.number, mergedAt: pr.mergedAt });
	const noise = dropped > 0 ? `, ${dropped} under a minute ignored` : '';
	console.log(`total\t${formatHours(seconds)}h\t${blocks} blocks\t${measured.length} session(s)${noise}`);
	console.log('');
	console.log(evidence);

	if (args.dryRun) {
		console.log('\ndry run: nothing dispatched');
		return;
	}

	const ref = args.ref ??
		ghJson(['repo', 'view', '--json', 'defaultBranchRef']).defaultBranchRef.name;
	const workflow = args.workflow ?? 'ai-actual.yml';
	const summary = measured.map((session) => `${session.name}=${toBlocks(session.seconds)}`).join(', ');
	for (const issue of issues) {
		try {
			gh([
				'workflow', 'run', workflow, '--repo', repo, '--ref', ref,
				'-f', `issue=${issue}`,
				'-f', `blocks=${blocks}`,
				'-f', `summary=${summary}`,
				'-f', `evidence=${evidence}`,
			]);
			console.log(`dispatched ${workflow} on ${ref} for #${issue} (${blocks} blocks)`);
		} catch (error) {
			console.error(`dispatch failed for #${issue}: ${error.message}`);
			console.error(`if the workflow is not on ${ref} yet, pass --ref <branch>`);
			process.exitCode = 1;
		}
	}
}

try {
	main();
} catch (error) {
	console.error(error.message);
	process.exitCode = 1;
}

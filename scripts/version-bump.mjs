#!/usr/bin/env node
// Advance frontend/package.json and CHANGELOG.md from conventional commits.
// Usage: node scripts/version-bump.mjs [--from <sha>] [--to <sha>]

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const packagePath = path.join(root, 'frontend', 'package.json');
const changelogPath = path.join(root, 'CHANGELOG.md');
const header = '# Changelog\n';
const recordSeparator = '\u001e';
const fieldSeparator = '\u001f';

const sections = [
  { title: 'Breaking Changes', match: (c) => c.breaking },
  { title: 'Features', match: (c) => !c.breaking && c.type === 'feat' },
  { title: 'Bug Fixes', match: (c) => !c.breaking && c.type === 'fix' },
  { title: 'Performance', match: (c) => !c.breaking && c.type === 'perf' },
  {
    title: 'Other Changes',
    match: (c) => !c.breaking && !['feat', 'fix', 'perf'].includes(c.type),
  },
];

export function parseCommit(subject, body = '') {
  const text = (subject ?? '').trim();
  const match = /^([A-Za-z]+)(?:\(([^)]+)\))?(!)?: (.+)$/.exec(text);
  if (!match) {
    return { type: 'other', scope: '', breaking: false, description: text };
  }
  return {
    type: match[1].toLowerCase(),
    scope: match[2] ?? '',
    breaking: Boolean(match[3]) || /^BREAKING[ -]CHANGE:/m.test(body),
    description: match[4].trim(),
  };
}

export function bumpLevel(commits) {
  const rank = { patch: 1, minor: 2, major: 3 };
  let level = null;
  const raise = (candidate) => {
    if (!level || rank[candidate] > rank[level]) level = candidate;
  };
  for (const commit of commits) {
    if (commit.breaking) raise('major');
    else if (commit.type === 'feat') raise('minor');
    else if (['fix', 'perf', 'revert'].includes(commit.type)) raise('patch');
  }
  return level;
}

export function nextVersion(version, level) {
  const match = /^(\d+)\.(\d+)\.(\d+)/.exec(version);
  if (!match) throw new Error(`unsupported version: ${version}`);
  const major = Number(match[1]);
  const minor = Number(match[2]);
  const patch = Number(match[3]);
  if (level === 'major') return `${major + 1}.0.0`;
  if (level === 'minor') return `${major}.${minor + 1}.0`;
  return `${major}.${minor}.${patch + 1}`;
}

export function renderEntry(version, date, commits) {
  const lines = [`## [${version}] - ${date}`, ''];
  for (const section of sections) {
    const matching = commits.filter(section.match);
    if (matching.length === 0) continue;
    lines.push(`### ${section.title}`, '');
    for (const commit of matching) {
      const scope = commit.scope ? `**${commit.scope}:** ` : '';
      const suffix = commit.short ? ` (${commit.short})` : '';
      lines.push(`- ${scope}${commit.description}${suffix}`);
    }
    lines.push('');
  }
  return `${lines.join('\n').trimEnd()}\n`;
}

export function prependEntry(existing, entry) {
  const trimmed = (existing ?? '').trim();
  if (!trimmed) return `${header}\n${entry}`;
  const body = trimmed.startsWith(header.trim())
    ? trimmed.slice(header.trim().length).replace(/^\s+/, '')
    : trimmed;
  return `${header}\n${entry}\n${body}\n`;
}

function git(args) {
  return execFileSync('git', args, { cwd: root, encoding: 'utf8' });
}

export function readCommits(from, to = 'HEAD') {
  const format = `--format=%h${fieldSeparator}%s${fieldSeparator}%b${recordSeparator}`;
  const range = from && !/^0+$/.test(from) ? `${from}..${to}` : null;
  const args = ['log', format];
  if (range) args.push(range);
  else args.push('-n', '1', to);
  return git(args)
    .split(recordSeparator)
    .map((record) => record.trim())
    .filter(Boolean)
    .map((record) => {
      const [short, subject, ...body] = record.split(fieldSeparator);
      const parsed = parseCommit(subject, body.join(fieldSeparator));
      return { ...parsed, short };
    });
}

function parseArgs(argv) {
  const args = { from: '', to: 'HEAD' };
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i] === '--from') args.from = argv[i + 1] ?? '';
    else if (argv[i] === '--to') args.to = argv[i + 1] ?? 'HEAD';
  }
  return args;
}

function main() {
  const { from, to } = parseArgs(process.argv.slice(2));
  const commits = readCommits(from, to);
  const level = bumpLevel(commits);
  if (!level) {
    console.log('version-bump: no bump-worthy commits');
    return;
  }
  const raw = readFileSync(packagePath, 'utf8');
  const current = JSON.parse(raw).version;
  const next = nextVersion(current, level);
  const updated = raw.replace(/("version"\s*:\s*")[^"]+(")/, `$1${next}$2`);
  writeFileSync(packagePath, updated);
  const date = new Date().toISOString().slice(0, 10);
  const existing = existsSync(changelogPath) ? readFileSync(changelogPath, 'utf8') : '';
  writeFileSync(changelogPath, prependEntry(existing, renderEntry(next, date, commits)));
  console.log(`version-bump: ${current} -> ${next} (${level})`);
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) main();

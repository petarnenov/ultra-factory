#!/usr/bin/env node
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const CHECKS = ['comment', 'issue', 'lint'];
const DEFAULT_BASE = 'origin/dev';
const isMain = process.argv[1]
  && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href;

class SetupError extends Error {}

export function parseOnly(value) {
  const names = String(value ?? '').split(',').map((name) => name.trim())
    .filter(Boolean);
  if (names.length === 0) throw new Error('--only needs at least one check');
  const unknown = names.filter((name) => !CHECKS.includes(name));
  if (unknown.length > 0) {
    throw new Error(`unknown check(s) in --only: ${unknown.join(', ')}`);
  }
  return [...new Set(names)];
}

function positiveInteger(value, flag) {
  const number = Number(value);
  if (!Number.isInteger(number) || number <= 0) {
    throw new Error(`${flag} needs a positive integer`);
  }
  return number;
}

export function parseArgs(argv) {
  const options = {
    only: [...CHECKS],
    pr: null,
    base: null,
    fetch: true,
    help: false,
    explicitOnly: false,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === '--help' || argument === '-h') {
      options.help = true;
    } else if (argument === '--pr') {
      index += 1;
      options.pr = positiveInteger(argv[index], '--pr');
    } else if (argument === '--base') {
      index += 1;
      if (!argv[index]) throw new Error('--base needs a git ref');
      options.base = argv[index];
    } else if (argument === '--only') {
      index += 1;
      options.only = parseOnly(argv[index]);
      options.explicitOnly = true;
    } else if (argument === '--no-fetch') {
      options.fetch = false;
    } else {
      throw new Error(`unknown argument: ${argument}`);
    }
  }
  return options;
}

export function repositoryFromRemote(remote) {
  const cleaned = String(remote ?? '').trim().replace(/\/+$/, '')
    .replace(/\.git$/, '');
  const match = /github\.com[:/]+([^/]+)\/([^/]+)$/.exec(cleaned);
  if (!match) {
    throw new SetupError(
      `cannot derive owner/repo from origin ${JSON.stringify(remote)}; `
      + 'set GITHUB_REPOSITORY.',
    );
  }
  return `${match[1]}/${match[2]}`;
}

export function goVersionFromMod(text) {
  const match = /^go[ \t]+(\S+)[ \t]*$/m.exec(text);
  if (!match) throw new SetupError('backend/go.mod has no go directive');
  return match[1];
}

export function toolchainFromGoVersion(version) {
  const parts = String(version).trim().split('.');
  const complete = parts.length === 2 ? `${version}.0` : version;
  return `go${complete}`;
}

function usage() {
  console.log(`Usage: node scripts/ci-check.mjs [options]

Runs the CI policy checks locally with the inputs CI uses.

Options:
  --pr <number>    Pull request to check (default: the current branch's PR)
  --base <ref>     Comment comparison base (default: the PR base SHA)
  --only <checks>  Comma-separated: comment, issue, lint (default: all)
  --no-fetch       Do not refresh the comparison base from origin
  -h, --help       Show this help

Checks:
  comment  comment-policy tests, changed comments, and the debt baseline
  issue    issue-policy tests, then the PR body and live issue labels
  lint     the go.yml lint job: golangci-lint v2.14.0 in backend

The comment and lint checks force GOTOOLCHAIN to backend/go.mod's version, as
actions/setup-go does in CI. Before the PR exists, the issue check is skipped;
run it again after opening the PR.`);
}

function run(command, args, { cwd = ROOT, env = process.env } = {}) {
  console.log(`==> ${command} ${args.join(' ')}`);
  const result = spawnSync(command, args, { cwd, env, stdio: 'inherit' });
  if (result.error?.code === 'ENOENT') {
    throw new SetupError(`${command} was not found on PATH`);
  }
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

function capture(command, args, { cwd = ROOT } = {}) {
  const result = spawnSync(command, args, { cwd, encoding: 'utf8' });
  if (result.error?.code === 'ENOENT') {
    throw new SetupError(`${command} was not found on PATH`);
  }
  return result;
}

function gitOutput(args) {
  const result = capture('git', args);
  if (result.status !== 0) {
    throw new SetupError(`git ${args.join(' ')} failed: ${result.stderr.trim()}`);
  }
  return result.stdout;
}

function commitExists(ref) {
  const result = spawnSync('git', ['rev-parse', '--verify', '--quiet', `${ref}^{commit}`],
    { cwd: ROOT, stdio: 'ignore' });
  return result.status === 0;
}

function fetchRef(ref) {
  const result = spawnSync('git', ['fetch', '--quiet', 'origin', ref],
    { cwd: ROOT, stdio: 'ignore' });
  return result.status === 0;
}

function pullBaseSha(pull) {
  const match = /github\.com\/([^/]+)\/([^/]+)\/pull\/\d+/.exec(pull.url || '');
  if (!match) return null;
  const result = spawnSync('gh', [
    'api', `repos/${match[1]}/${match[2]}/pulls/${pull.number}`,
    '--jq', '.base.sha',
  ], { cwd: ROOT, encoding: 'utf8' });
  return result.status === 0 ? result.stdout.trim() || null : null;
}

function resolvePull(options) {
  const branch = gitOutput(['rev-parse', '--abbrev-ref', 'HEAD']).trim();
  const args = options.pr
    ? ['pr', 'view', String(options.pr), '--json', 'number,baseRefName,url']
    : ['pr', 'list', '--head', branch, '--state', 'open', '--limit', '1',
      '--json', 'number,baseRefName,url'];
  const result = spawnSync('gh', args, { cwd: ROOT, encoding: 'utf8' });
  if (result.error?.code === 'ENOENT') {
    return { pull: null, reason: 'gh was not found on PATH', unavailable: true };
  }
  if (result.status !== 0) {
    const reason = result.stderr.trim().split('\n')[0];
    return { pull: null, reason: reason || 'gh failed', unavailable: true };
  }
  try {
    const parsed = JSON.parse(result.stdout);
    const pull = Array.isArray(parsed) ? parsed[0] : parsed;
    if (!pull) {
      return {
        pull: null,
        reason: `no open pull request for ${branch}`,
        unavailable: false,
      };
    }
    return {
      pull: { ...pull, baseRefOid: pullBaseSha(pull) },
      reason: null,
      unavailable: false,
    };
  } catch {
    throw new SetupError('gh did not return valid pull request JSON');
  }
}

function ghAuthToken() {
  const result = spawnSync('gh', ['auth', 'token'], { cwd: ROOT, encoding: 'utf8' });
  return result.status === 0 ? result.stdout.trim() || null : null;
}

function requireCommit(ref, source) {
  if (!commitExists(ref)) {
    throw new SetupError(
      `comparison base ${ref} (${source}) is not a local commit; fetch it or `
      + 'pass a valid --base.',
    );
  }
  return { base: ref, source };
}

function resolveCommentBase(options, pull) {
  if (options.base) return requireCommit(options.base, '--base');
  if (process.env.COMMENT_POLICY_BASE) {
    return requireCommit(process.env.COMMENT_POLICY_BASE, 'COMMENT_POLICY_BASE');
  }
  if (pull?.baseRefOid) {
    const source = `pull request #${pull.number} base`;
    if (!commitExists(pull.baseRefOid) && options.fetch && pull.baseRefName) {
      fetchRef(pull.baseRefName);
    }
    if (commitExists(pull.baseRefOid)) return { base: pull.baseRefOid, source };
    if (pull.baseRefName && commitExists(`origin/${pull.baseRefName}`)) {
      return {
        base: `origin/${pull.baseRefName}`,
        source: 'pull request base branch (exact commit unavailable)',
      };
    }
  }
  if (pull?.baseRefName) {
    if (options.fetch) fetchRef(pull.baseRefName);
    if (commitExists(`origin/${pull.baseRefName}`)) {
      return {
        base: `origin/${pull.baseRefName}`,
        source: 'pull request base branch',
      };
    }
  }
  if (options.fetch) fetchRef('dev');
  if (!commitExists(DEFAULT_BASE)) {
    throw new SetupError(
      `comparison base ${DEFAULT_BASE} is missing; run git fetch origin dev.`,
    );
  }
  return { base: DEFAULT_BASE, source: 'default integration branch' };
}

function requireNode24() {
  const major = Number(process.versions.node.split('.')[0]);
  if (major < 24) {
    throw new SetupError(
      `Node.js 24 or newer is required (CI uses 24); found ${process.version}.`,
    );
  }
}

function requireFrontendTypeScript() {
  const manifest = path.join(ROOT, 'frontend', 'node_modules', 'typescript',
    'package.json');
  if (existsSync(manifest)) return;
  throw new SetupError(
    'comment-policy needs the TypeScript compiler from frontend/node_modules.\n'
    + 'Install it as the comment-policy CI job does:\n'
    + '  npm ci --prefix frontend --ignore-scripts\n'
    + 'or run make setup.',
  );
}

function requirePython3() {
  const result = spawnSync('python3', ['--version'], { cwd: ROOT, stdio: 'ignore' });
  if (result.error?.code === 'ENOENT') {
    throw new SetupError(
      'python3 was not found on PATH; it scans Python, shell and Dockerfile '
      + 'comments. Install Python 3.',
    );
  }
}

function requireGo() {
  const result = spawnSync('go', ['version'], { cwd: ROOT, stdio: 'ignore' });
  if (result.error?.code === 'ENOENT') {
    throw new SetupError('Go was not found on PATH; install the version in go.mod.');
  }
}

function goEnvironment() {
  const version = goVersionFromMod(
    readFileSync(path.join(ROOT, 'backend', 'go.mod'), 'utf8'),
  );
  const toolchain = toolchainFromGoVersion(version);
  return { version, toolchain, env: { ...process.env, GOTOOLCHAIN: toolchain } };
}

function runComment(options, env, pull) {
  requireFrontendTypeScript();
  requirePython3();
  requireGo();
  const { base, source } = resolveCommentBase(options, pull);
  console.log(`ci-check: comment base ${base} (${source})`);
  run(process.execPath, ['--test', 'scripts/comment-policy/check.test.mjs'], { env });
  run('go', ['test', './cmd/commentpolicy'],
    { cwd: path.join(ROOT, 'backend'), env });
  run(process.execPath, ['scripts/comment-policy/check.mjs'],
    { env: { ...env, COMMENT_POLICY_BASE: base } });
}

function runIssue(options, env, { pull, reason, unavailable }) {
  run(process.execPath, ['--test', 'scripts/issue-policy/check.test.mjs'], { env });
  const number = options.pr || pull?.number || Number(env.PR_NUMBER) || null;
  if (!number) {
    if (unavailable) {
      throw new SetupError(
        `cannot resolve a pull request: ${reason}. Install and authenticate `
        + 'gh, or pass --pr <number> and GH_TOKEN.',
      );
    }
    if (options.explicitOnly && options.only.length === 1) {
      throw new SetupError(
        'the issue check needs an open pull request; push the branch and open '
        + 'the PR, or pass --pr <number>.',
      );
    }
    console.log(
      `ci-check: issue policy skipped (${reason}; run again after opening `
      + 'the PR)',
    );
    return;
  }
  const repository = env.GITHUB_REPOSITORY
    || repositoryFromRemote(gitOutput(['remote', 'get-url', 'origin']).trim());
  const token = env.GH_TOKEN || env.GITHUB_TOKEN || ghAuthToken();
  if (!token) {
    throw new SetupError(
      'set GH_TOKEN (for example GH_TOKEN=$(gh auth token)) or run gh auth login.',
    );
  }
  console.log(`ci-check: issue policy for #${number} in ${repository}`);
  run(process.execPath, ['scripts/issue-policy/check.mjs'], {
    env: {
      ...env,
      GH_TOKEN: token,
      GITHUB_REPOSITORY: repository,
      PR_NUMBER: String(number),
    },
  });
}

function runLint(env) {
  requireGo();
  run('bash', ['scripts/lint.sh', '--lint-only'], { env });
}

function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    usage();
    return;
  }
  process.chdir(ROOT);
  requireNode24();
  const { toolchain, env } = goEnvironment();
  console.log(`ci-check: Go toolchain ${toolchain} (from backend/go.mod)`);
  const needsPull = options.only.includes('comment') || options.only.includes('issue');
  const resolved = needsPull
    ? resolvePull(options)
    : { pull: null, reason: null, unavailable: false };
  if (options.only.includes('comment') && !resolved.pull && resolved.reason) {
    console.log(`ci-check: ${resolved.reason}`);
  }
  if (options.only.includes('comment')) runComment(options, env, resolved.pull);
  if (options.only.includes('issue')) runIssue(options, env, resolved);
  if (options.only.includes('lint')) runLint(env);
  console.log('\nci-check: selected checks passed');
}

if (isMain) {
  try {
    main();
  } catch (error) {
    if (error instanceof SetupError) {
      console.error(`ci-check: ${error.message}`);
      process.exit(2);
    }
    console.error(`ci-check: ${error.stack || error.message}`);
    process.exit(1);
  }
}

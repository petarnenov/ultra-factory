#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

// The type and priority taxonomy lives here and in docs/issue-policy.md.
export const TYPE_LABELS = ['accessibility', 'bug', 'chore', 'documentation', 'enhancement'];
export const PRIORITY_LABELS = [
  'priority:critical',
  'priority:high',
  'priority:medium',
  'priority:low',
];
export const DEFAULT_TYPE = 'enhancement';
export const DEFAULT_PRIORITY = 'priority:medium';
export const PROMOTION_BASE = 'main';
export const PROMOTION_HEAD = 'dev';
export const AGENT_PIN_BRANCH = 'chore/agent-cli-pins';

const typeSet = new Set(TYPE_LABELS);
const prioritySet = new Set(PRIORITY_LABELS);
const isMain = process.argv[1]
  && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href;

// Fenced blocks and inline code hold literal text such as a placeholder or a
// quoted message, not references. Both are dropped before extraction.
export function prosaicText(text) {
  return String(text ?? '')
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`[^`\n]*`/g, ' ');
}

// Cross-repository references (owner/repo#123) are not checked here.
export function extractIssueReferences(...texts) {
  const numbers = [];
  for (const text of texts) {
    for (const match of prosaicText(text).matchAll(/(?<![\w/#])#(\d+)\b/g)) {
      const number = Number(match[1]);
      if (!numbers.includes(number)) numbers.push(number);
    }
  }
  return numbers;
}

// Two narrow exemptions, both inside this repository: a fork branch name is
// never trusted. Keep in sync with the docs.
export function exemptionFor(pull) {
  const baseRepo = pull?.base?.repo?.full_name;
  const headRepo = pull?.head?.repo?.full_name;
  if (!baseRepo || baseRepo !== headRepo) return null;
  const base = pull?.base?.ref;
  const head = pull?.head?.ref;
  if (base === PROMOTION_BASE && head === PROMOTION_HEAD) {
    return 'promotion of dev into main: the batch record, not a single change';
  }
  if (head === AGENT_PIN_BRANCH) {
    return 'automated agent-pin refresh: the workflow owns the change and its record';
  }
  return null;
}

function missingLabelMessage(number, reference, kind, suggested) {
  return `#${number} references #${reference}, which has no ${kind} label: `
    + `gh issue edit ${reference} --add-label ${suggested}`;
}

function extraLabelMessage(number, reference, kind, labels) {
  const remove = labels.slice(1).map((label) => `--remove-label ${label}`).join(' ');
  return `#${number} references #${reference}, which has ${labels.length} ${kind} `
    + `labels (${labels.join(', ')}): gh issue edit ${reference} ${remove}`;
}

// `issues` maps every referenced number to `{ labels, isPullRequest }` or null.
export function evaluatePullRequest({ number, references, issues }) {
  const failures = [];
  const referencedIssues = references.filter(
    (reference) => issues.get(reference)?.isPullRequest !== true,
  );
  if (referencedIssues.length === 0) {
    failures.push(`#${number} references no issue: add "Closes #123" to the description`);
    return failures;
  }
  for (const reference of referencedIssues) {
    const issue = issues.get(reference);
    if (!issue) {
      failures.push(`#${number} references #${reference}, which does not exist in `
        + 'this repository: fix the reference or open the issue first');
      continue;
    }
    const types = issue.labels.filter((label) => typeSet.has(label)).sort();
    const priorities = issue.labels.filter((label) => prioritySet.has(label)).sort();
    if (types.length === 0) {
      failures.push(missingLabelMessage(number, reference, 'type', DEFAULT_TYPE));
    } else if (types.length > 1) {
      failures.push(extraLabelMessage(number, reference, 'type', types));
    }
    if (priorities.length === 0) {
      failures.push(missingLabelMessage(number, reference, 'priority', DEFAULT_PRIORITY));
    } else if (priorities.length > 1) {
      failures.push(extraLabelMessage(number, reference, 'priority', priorities));
    }
  }
  return failures;
}

function parseArguments(argv) {
  const index = argv.indexOf('--pr');
  if (index === -1) return null;
  const value = Number(argv[index + 1]);
  return Number.isInteger(value) && value > 0 ? value : null;
}

function pullNumberFromEnvironment() {
  const explicit = Number(process.env.PR_NUMBER);
  if (Number.isInteger(explicit) && explicit > 0) return explicit;
  const eventPath = process.env.GITHUB_EVENT_PATH;
  if (!eventPath || !fs.existsSync(eventPath)) return null;
  const event = JSON.parse(fs.readFileSync(eventPath, 'utf8'));
  const number = event.pull_request?.number;
  return Number.isInteger(number) ? number : null;
}

function apiHeaders(token) {
  return {
    accept: 'application/vnd.github+json',
    authorization: `Bearer ${token}`,
    'user-agent': 'ao-issue-policy',
    'x-github-api-version': '2022-11-28',
  };
}

async function apiGet(apiBase, pathname, token) {
  const response = await fetch(`${apiBase}${pathname}`, { headers: apiHeaders(token) });
  if (response.status === 404) return null;
  if (!response.ok) {
    const body = await response.text();
    throw new Error(`GitHub API ${response.status} for ${pathname}: ${body}`);
  }
  return response.json();
}

async function main() {
  const token = process.env.GH_TOKEN || process.env.GITHUB_TOKEN;
  const repo = process.env.GITHUB_REPOSITORY;
  const apiBase = process.env.GITHUB_API_URL || 'https://api.github.com';
  const number = parseArguments(process.argv.slice(2)) ?? pullNumberFromEnvironment();
  if (!number) {
    console.error('No pull request number: pass --pr <number> or set PR_NUMBER.');
    process.exitCode = 2;
    return;
  }
  if (!token || !repo) {
    console.error('Set GH_TOKEN (or GITHUB_TOKEN) and GITHUB_REPOSITORY.');
    process.exitCode = 2;
    return;
  }

  const pull = await apiGet(apiBase, `/repos/${repo}/pulls/${number}`, token);
  if (!pull) {
    console.error(`Pull request #${number} was not found in ${repo}.`);
    process.exitCode = 1;
    return;
  }
  const exemption = exemptionFor(pull);
  if (exemption) {
    console.log(`#${number} is exempt from the issue policy: ${exemption}.`);
    return;
  }

  const references = extractIssueReferences(pull.title, pull.body);
  const issues = new Map();
  for (const reference of references) {
    const issue = await apiGet(apiBase, `/repos/${repo}/issues/${reference}`, token);
    issues.set(reference, issue && {
      labels: issue.labels.map((label) => label.name),
      isPullRequest: issue.pull_request !== undefined,
    });
  }

  const failures = evaluatePullRequest({ number, references, issues });
  if (failures.length === 0) {
    console.log(`#${number} references its issue with a type and a priority label.`);
    return;
  }
  for (const failure of failures) console.error(failure);
  console.error('See docs/issue-policy.md for the type and priority labels.');
  process.exitCode = 1;
}

if (isMain) await main();

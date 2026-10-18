// Keeps the agent CLIs pinned in the Railway Dockerfiles at npm latest.
// The scheduled workflow in .github/workflows/agent-cli-pins.yml runs it.

import { appendFile, readFile, writeFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

const REGISTRY = "https://registry.npmjs.org";
const VERSION = /^\d+\.\d+\.\d+$/;

// Every ARG whose value is whitespace-separated name@version pins.
export const PINNED_ARGS = [
  { file: "deploy/railway/worker/Dockerfile", arg: "AGENT_NPM_PACKAGES" },
  { file: "deploy/railway/hub/Dockerfile", arg: "HUB_AGENT_NPM_PACKAGES" },
  { file: "deploy/railway/hub/Dockerfile", arg: "HUB_OPENCODE_V2_PACKAGE" },
];

// Worker OpenCode stays on v1; v2 is the hub's separate @opencode/cli.
export const PINNED_MAJORS = { "opencode-ai": 1 };

// The worker README repeats the ARG default; keep its table row in sync.
export const PINNED_DOCS = [
  {
    file: "deploy/railway/worker/README.md",
    arg: "AGENT_NPM_PACKAGES",
    source: "deploy/railway/worker/Dockerfile",
  },
];

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function argLine(arg) {
  return new RegExp(`^(ARG[ \\t]+${escapeRegExp(arg)}=)"([^"\\n]*)"[ \\t]*$`, "m");
}

export function readPinnedValue(content, arg) {
  const match = argLine(arg).exec(content);
  if (!match) throw new Error(`missing single-line ARG ${arg}`);
  return match[2];
}

export function parsePins(value) {
  return value.trim().split(/\s+/).filter(Boolean).map((token) => {
    const at = token.lastIndexOf("@");
    if (at <= 0 || !VERSION.test(token.slice(at + 1))) {
      throw new Error(`invalid pinned package: ${token}`);
    }
    return { token, name: token.slice(0, at), version: token.slice(at + 1) };
  });
}

export function rewritePinnedValue(content, arg, updates) {
  const value = readPinnedValue(content, arg);
  let next = value;
  for (const update of updates) {
    next = next.split(`${update.name}@${update.from}`).join(`${update.name}@${update.to}`);
  }
  if (next === value) return content;
  return content.replace(argLine(arg), (_line, prefix) => `${prefix}"${next}"`);
}

export function rewriteDocValue(content, arg, value) {
  const row = new RegExp(`^(\\| \`${escapeRegExp(arg)}\` \\| )\`[^\`]*\`( \\|)`, "m");
  if (!row.test(content)) throw new Error(`missing docs row for ${arg}`);
  return content.replace(row, `$1\`${value}\`$2`);
}

export function compareVersions(left, right) {
  const a = left.split(".").map(Number);
  const b = right.split(".").map(Number);
  for (let index = 0; index < 3; index += 1) {
    if (a[index] !== b[index]) return Math.sign(a[index] - b[index]);
  }
  return 0;
}

async function fetchJson(fetchImpl, url, accept = "application/json") {
  const response = await fetchImpl(url, { headers: { accept } });
  if (!response.ok) throw new Error(`npm registry ${response.status}: ${url}`);
  return response.json();
}

export async function resolveLatest(name, fetchImpl = fetch) {
  const major = PINNED_MAJORS[name];
  const url = major === undefined
    ? `${REGISTRY}/${encodeURIComponent(name)}/latest`
    : `${REGISTRY}/${encodeURIComponent(name)}`;
  const accept = major === undefined
    ? "application/json"
    : "application/vnd.npm.install-v1+json";
  const metadata = await fetchJson(fetchImpl, url, accept);
  if (major === undefined) {
    if (!VERSION.test(metadata.version ?? "")) {
      throw new Error(`npm registry returned no stable latest version for ${name}`);
    }
    return metadata.version;
  }
  const versions = Object.keys(metadata.versions ?? {})
    .filter((version) => VERSION.test(version) && Number(version.split(".")[0]) === major);
  if (!versions.length) throw new Error(`no ${major}.x release for ${name}`);
  return versions.sort(compareVersions).at(-1);
}

export async function planPins({ readFileImpl = readFile, fetchImpl = fetch } = {}) {
  const files = new Map();
  const pins = [];
  for (const target of PINNED_ARGS) {
    const content = files.get(target.file) ?? await readFileImpl(target.file, "utf8");
    files.set(target.file, content);
    for (const pin of parsePins(readPinnedValue(content, target.arg))) {
      pins.push({ ...target, ...pin });
    }
  }

  const latest = new Map();
  for (const pin of pins) {
    if (!latest.has(pin.name)) latest.set(pin.name, await resolveLatest(pin.name, fetchImpl));
  }

  const updates = [];
  const skipped = [];
  for (const pin of pins) {
    const target = latest.get(pin.name);
    if (target === pin.version) continue;
    if (compareVersions(target, pin.version) < 0) {
      skipped.push({ ...pin, from: pin.version, target, reason: "npm latest is older" });
      continue;
    }
    updates.push({ ...pin, from: pin.version, to: target });
  }
  return { files, updates, skipped };
}

export function formatSummary(updates, skipped = []) {
  const lines = [
    "Resolved npm `latest` for the agent CLIs pinned in the Railway images.",
    "",
    "| Dockerfile | ARG | Package | From | To |",
    "| --- | --- | --- | --- | --- |",
    ...updates.map((update) =>
      `| \`${update.file}\` | \`${update.arg}\` | \`${update.name}\` | ` +
      `${update.from} | ${update.to} |`),
  ];
  if (skipped.length) {
    lines.push("", "Not updated:", "");
    for (const item of skipped) {
      lines.push(`- \`${item.name}\` ${item.version}: ${item.reason} (\`${item.target}\`)`);
    }
  }
  return `${lines.join("\n")}\n`;
}

async function main(argv) {
  const write = argv.includes("--write");
  const summaryIndex = argv.indexOf("--summary");
  const summaryPath = summaryIndex === -1 ? null : argv[summaryIndex + 1];
  if (summaryIndex !== -1 && !summaryPath) throw new Error("--summary needs a path");

  const { files, updates, skipped } = await planPins();
  const summary = formatSummary(updates, skipped);
  process.stdout.write(summary);
  if (process.env.GITHUB_STEP_SUMMARY) {
    await appendFile(process.env.GITHUB_STEP_SUMMARY, summary);
  }

  const changed = new Map();
  for (const [file, content] of files) {
    let next = content;
    for (const update of updates.filter((item) => item.file === file)) {
      next = rewritePinnedValue(next, update.arg, [update]);
    }
    if (next !== content) changed.set(file, next);
  }

  for (const doc of PINNED_DOCS) {
    if (!changed.has(doc.source)) continue;
    const source = changed.get(doc.source);
    const value = readPinnedValue(source, doc.arg);
    const content = await readFile(doc.file, "utf8");
    const next = rewriteDocValue(content, doc.arg, value);
    if (next !== content) changed.set(doc.file, next);
  }

  if (write) {
    for (const [file, content] of changed) await writeFile(file, content);
  }
  if (summaryPath) await writeFile(summaryPath, summary);
  if (!updates.length) process.stdout.write("All agent CLI pins are current.\n");
}

const isMain = process.argv[1]
  && import.meta.url === pathToFileURL(process.argv[1]).href;

if (isMain) {
  main(process.argv.slice(2)).catch((error) => {
    console.error(error instanceof Error ? error.message : error);
    process.exitCode = 1;
  });
}

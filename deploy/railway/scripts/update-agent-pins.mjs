#!/usr/bin/env node
// Refresh the agent CLI pins in the Railway worker and hub images.
// Pins are exact versions in Dockerfile ARGs; this keeps them on npm latest.

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const repoRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../..",
);

// Every target has a named `pins` group: the space-separated
// `package@version` list that the script rewrites.
export const targets = [
  {
    file: "deploy/railway/worker/Dockerfile",
    arg: "AGENT_NPM_PACKAGES",
    pattern: /^ARG AGENT_NPM_PACKAGES="(?<pins>[^"]*)"/m,
    render: (pins) => `ARG AGENT_NPM_PACKAGES="${pins}"`,
  },
  {
    file: "deploy/railway/worker/README.md",
    arg: "AGENT_NPM_PACKAGES",
    pattern: /^(\| `AGENT_NPM_PACKAGES` \| `)(?<pins>[^`]*)(` \|)/m,
    render: (pins, match) => `${match[1]}${pins}${match[3]}`,
  },
  {
    file: "deploy/railway/hub/Dockerfile",
    arg: "HUB_AGENT_NPM_PACKAGES",
    pattern: /^ARG HUB_AGENT_NPM_PACKAGES="(?<pins>[^"]*)"/m,
    render: (pins) => `ARG HUB_AGENT_NPM_PACKAGES="${pins}"`,
  },
  {
    file: "deploy/railway/hub/Dockerfile",
    arg: "HUB_OPENCODE_V2_PACKAGE",
    pattern: /^ARG HUB_OPENCODE_V2_PACKAGE="(?<pins>[^"]*)"/m,
    render: (pins) => `ARG HUB_OPENCODE_V2_PACKAGE="${pins}"`,
  },
];

const versionPattern = /^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/;

export function parsePackages(value) {
  return value
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map((spec) => {
      const at = spec.lastIndexOf("@");
      if (at <= 0) {
        throw new Error(`package spec has no version: ${spec}`);
      }
      return { name: spec.slice(0, at), version: spec.slice(at + 1) };
    });
}

export function formatPackages(packages) {
  return packages.map((entry) => `${entry.name}@${entry.version}`).join(" ");
}

function findPins(content, target) {
  const match = target.pattern.exec(content);
  if (!match) {
    throw new Error(`${target.file}: ${target.arg} line not found`);
  }
  return match;
}

export function applyTarget(content, target, versions) {
  const match = findPins(content, target);
  const changes = [];
  const packages = parsePackages(match.groups.pins).map((entry) => {
    if (!(entry.name in versions)) {
      throw new Error(`no resolved version for ${entry.name}`);
    }
    const version = versions[entry.name];
    if (!versionPattern.test(version)) {
      throw new Error(`invalid version for ${entry.name}: ${version}`);
    }
    if (version === entry.version) {
      return entry;
    }
    changes.push({ name: entry.name, from: entry.version, to: version });
    return { name: entry.name, version };
  });
  const rendered = target.render(formatPackages(packages), match);
  const text =
    content.slice(0, match.index) +
    rendered +
    content.slice(match.index + match[0].length);
  return { text, changes };
}

export function collectPackages(root = repoRoot) {
  const names = new Set();
  const contents = new Map();
  for (const target of targets) {
    if (!contents.has(target.file)) {
      contents.set(target.file, readFileSync(join(root, target.file), "utf8"));
    }
    const match = findPins(contents.get(target.file), target);
    for (const entry of parsePackages(match.groups.pins)) {
      names.add(entry.name);
    }
  }
  return [...names].sort();
}

export function updatePins({ root = repoRoot, versions, write = false }) {
  const originals = new Map();
  const contents = new Map();
  const changes = [];
  for (const target of targets) {
    if (!contents.has(target.file)) {
      const content = readFileSync(join(root, target.file), "utf8");
      originals.set(target.file, content);
      contents.set(target.file, content);
    }
    const result = applyTarget(contents.get(target.file), target, versions);
    contents.set(target.file, result.text);
    for (const change of result.changes) {
      changes.push({ file: target.file, arg: target.arg, ...change });
    }
  }
  if (write) {
    for (const [file, content] of contents) {
      if (content !== originals.get(file)) {
        writeFileSync(join(root, file), content);
      }
    }
  }
  return { changes, contents };
}

export function npmLatest(name) {
  const output = execFileSync(
    "npm",
    ["view", `${name}@latest`, "version"],
    { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] },
  ).trim();
  const version = output.split("\n").pop().trim();
  if (!versionPattern.test(version)) {
    throw new Error(`npm returned an invalid version for ${name}: ${output}`);
  }
  return version;
}

export function summaryMarkdown(changes) {
  if (!changes.length) {
    return "All agent CLI pins already match npm latest.\n";
  }
  const rows = changes.map(
    (change) =>
      `| \`${change.file}\` | \`${change.arg}\` | \`${change.name}\` | ` +
      `${change.from} | ${change.to} |`,
  );
  return [
    "| File | ARG | Package | From | To |",
    "| --- | --- | --- | --- | --- |",
    ...rows,
    "",
  ].join("\n");
}

export function parseArgs(argv) {
  const options = { root: repoRoot, check: false, dryRun: false };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--check") {
      options.check = true;
    } else if (arg === "--dry-run") {
      options.dryRun = true;
    } else if (arg === "--help" || arg === "-h") {
      options.help = true;
    } else if (["--root", "--versions", "--summary"].includes(arg)) {
      const value = argv[i + 1];
      if (!value) {
        throw new Error(`${arg} needs a value`);
      }
      if (arg === "--root") options.root = value;
      if (arg === "--versions") options.versionsPath = value;
      if (arg === "--summary") options.summaryPath = value;
      i += 1;
    } else {
      throw new Error(`unknown argument: ${arg}`);
    }
  }
  return options;
}

export function run(options = {}) {
  if (options.help) {
    console.log(
      "Usage: update-agent-pins.mjs [--check] [--dry-run] " +
        "[--root DIR] [--versions FILE] [--summary FILE]",
    );
    return 0;
  }
  const root = resolve(options.root ?? repoRoot);
  const versions = options.versionsPath
    ? JSON.parse(readFileSync(options.versionsPath, "utf8"))
    : Object.fromEntries(
        collectPackages(root).map((name) => [name, npmLatest(name)]),
      );
  const write = !options.check && !options.dryRun;
  const { changes } = updatePins({ root, versions, write });
  if (options.summaryPath) {
    writeFileSync(options.summaryPath, summaryMarkdown(changes));
  }
  if (!changes.length) {
    console.log("All agent CLI pins already match npm latest.");
    return 0;
  }
  for (const change of changes) {
    console.log(
      `${change.file} ${change.arg}: ${change.name} ` +
        `${change.from} -> ${change.to}`,
    );
  }
  if (options.check) {
    console.log("Agent CLI pins are out of date.");
    return 1;
  }
  if (options.dryRun) {
    console.log("Dry run: no files written.");
  }
  return 0;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    process.exitCode = run(parseArgs(process.argv.slice(2)));
  } catch (error) {
    console.error(`update-agent-pins: ${error.message}`);
    process.exitCode = 1;
  }
}

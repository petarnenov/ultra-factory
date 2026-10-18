import assert from "node:assert/strict";
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { test } from "node:test";

import {
  applyTarget,
  collectPackages,
  formatPackages,
  parsePackages,
  repoRoot,
  run,
  summaryMarkdown,
  targets,
  updatePins,
} from "./update-agent-pins.mjs";

const workerDockerfile = `# syntax=docker/dockerfile:1
ARG GO_IMAGE=golang:1.27.1-bookworm
ARG AGENT_NPM_PACKAGES="@anthropic-ai/claude-code@2.1.228 @openai/codex@0.147.0 opencode-ai@1.19.3"
ARG SKIP_AGENT_INSTALL=0
`;

const workerReadme = `# worker
| Argument | Default | Purpose |
|---|---|---|
| \`AGENT_NPM_PACKAGES\` | \`@anthropic-ai/claude-code@2.1.228 @openai/codex@0.147.0 opencode-ai@1.19.3\` | Pinned global npm installs. |
`;

const hubDockerfile = `# syntax=docker/dockerfile:1
ARG HUB_AGENT_NPM_PACKAGES="@anthropic-ai/claude-code@2.1.302 @openai/codex@0.163.0"
ARG HUB_OPENCODE_V2_PACKAGE="@opencode/cli@2.0.32"
`;

const latest = {
  "@anthropic-ai/claude-code": "2.1.302",
  "@openai/codex": "0.163.0",
  "@opencode/cli": "2.0.32",
  "opencode-ai": "1.19.3",
};

const fixtures = {
  "deploy/railway/worker/Dockerfile": workerDockerfile,
  "deploy/railway/worker/README.md": workerReadme,
  "deploy/railway/hub/Dockerfile": hubDockerfile,
};

function makeRoot(t) {
  const root = mkdtempSync(join(tmpdir(), "agent-pins-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const [file, content] of Object.entries(fixtures)) {
    const path = join(root, file);
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, content);
  }
  return root;
}

function targetFor(file, arg) {
  const target = targets.find((item) => item.file === file && item.arg === arg);
  assert.ok(target, `missing target ${file} ${arg}`);
  return target;
}

test("parsePackages keeps scoped names intact", () => {
  assert.deepEqual(parsePackages("@scope/name@1.2.3 plain@0.4.5"), [
    { name: "@scope/name", version: "1.2.3" },
    { name: "plain", version: "0.4.5" },
  ]);
  assert.equal(
    formatPackages(parsePackages("@scope/name@1.2.3 plain@0.4.5")),
    "@scope/name@1.2.3 plain@0.4.5",
  );
});

test("the worker Dockerfile keeps unpinned packages in place", () => {
  const target = targetFor(
    "deploy/railway/worker/Dockerfile",
    "AGENT_NPM_PACKAGES",
  );
  const result = applyTarget(workerDockerfile, target, latest);
  assert.match(
    result.text,
    /ARG AGENT_NPM_PACKAGES="@anthropic-ai\/claude-code@2\.1\.302 @openai\/codex@0\.163\.0 opencode-ai@1\.19\.3"/,
  );
  assert.deepEqual(result.changes, [
    { name: "@anthropic-ai/claude-code", from: "2.1.228", to: "2.1.302" },
    { name: "@openai/codex", from: "0.147.0", to: "0.163.0" },
  ]);
});

test("the worker README default follows the Dockerfile", () => {
  const target = targetFor("deploy/railway/worker/README.md", "AGENT_NPM_PACKAGES");
  const result = applyTarget(workerReadme, target, latest);
  assert.match(result.text, /claude-code@2\.1\.302/);
  assert.match(result.text, /codex@0\.163\.0/);
  assert.match(result.text, /opencode-ai@1\.19\.3/);
});

test("the hub Dockerfile updates both agent pins", () => {
  const agentTarget = targetFor(
    "deploy/railway/hub/Dockerfile",
    "HUB_AGENT_NPM_PACKAGES",
  );
  const openCodeTarget = targetFor(
    "deploy/railway/hub/Dockerfile",
    "HUB_OPENCODE_V2_PACKAGE",
  );
  const agentResult = applyTarget(hubDockerfile, agentTarget, latest);
  const openCodeResult = applyTarget(agentResult.text, openCodeTarget, latest);
  assert.match(openCodeResult.text, /HUB_OPENCODE_V2_PACKAGE="@opencode\/cli@2\.0\.32"/);
  assert.deepEqual(openCodeResult.changes, []);
});

test("collectPackages sees every pinned package once", (t) => {
  const root = makeRoot(t);
  assert.deepEqual(collectPackages(root), [
    "@anthropic-ai/claude-code",
    "@openai/codex",
    "@opencode/cli",
    "opencode-ai",
  ]);
});

test("updatePins writes every target and is idempotent", (t) => {
  const root = makeRoot(t);
  const first = updatePins({ root, versions: latest, write: true });
  assert.equal(first.changes.length, 4);
  assert.match(
    first.contents.get("deploy/railway/worker/README.md"),
    /claude-code@2\.1\.302/,
  );
  const second = updatePins({ root, versions: latest, write: true });
  assert.deepEqual(second.changes, []);
});

test("check mode reports drift without writing", (t) => {
  const root = makeRoot(t);
  const versionsPath = join(root, "versions.json");
  writeFileSync(versionsPath, JSON.stringify(latest));
  const code = run({ root, versionsPath, check: true });
  assert.equal(code, 1);
  assert.match(
    readFileSync(join(root, "deploy/railway/worker/Dockerfile"), "utf8"),
    /claude-code@2\.1\.228/,
  );
});

test("a missing ARG line fails loudly", () => {
  const target = targetFor(
    "deploy/railway/worker/Dockerfile",
    "AGENT_NPM_PACKAGES",
  );
  assert.throws(
    () => applyTarget("FROM scratch\n", target, latest),
    /AGENT_NPM_PACKAGES line not found/,
  );
});

test("summaryMarkdown lists each change", () => {
  const summary = summaryMarkdown([
    {
      file: "deploy/railway/worker/Dockerfile",
      arg: "AGENT_NPM_PACKAGES",
      name: "@openai/codex",
      from: "0.147.0",
      to: "0.163.0",
    },
  ]);
  assert.match(summary, /^\| File \| ARG \| Package \| From \| To \|/);
  assert.match(summary, /@openai\/codex/);
});

test("the repository targets all four pinned packages", () => {
  assert.deepEqual(collectPackages(repoRoot), [
    "@anthropic-ai/claude-code",
    "@openai/codex",
    "@opencode/cli",
    "opencode-ai",
  ]);
});

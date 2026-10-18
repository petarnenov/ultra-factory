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
  compareVersions,
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

test("compareVersions orders versions numerically", () => {
  assert.equal(compareVersions("2.1.10", "2.1.9"), 1);
  assert.equal(compareVersions("2.1.9", "2.1.10"), -1);
  assert.equal(compareVersions("2.1.302", "2.1.302"), 0);
  assert.equal(compareVersions("2.1.302-beta.2", "2.1.302"), -1);
  assert.equal(compareVersions("2.1.302-beta.10", "2.1.302-beta.9"), 1);
});

test("a numerically newer minor is an update", () => {
  const target = targetFor(
    "deploy/railway/worker/Dockerfile",
    "AGENT_NPM_PACKAGES",
  );
  const content = workerDockerfile.replace("2.1.228", "2.1.9");
  const result = applyTarget(content, target, {
    ...latest,
    "@anthropic-ai/claude-code": "2.1.10",
  });
  assert.match(result.text, /claude-code@2\.1\.10/);
  assert.ok(
    result.changes.some(
      (change) => change.name === "@anthropic-ai/claude-code",
    ),
  );
});

test("refuses to move a pin backwards", () => {
  const target = targetFor(
    "deploy/railway/worker/Dockerfile",
    "AGENT_NPM_PACKAGES",
  );
  assert.throws(
    () =>
      applyTarget(workerDockerfile, target, {
        ...latest,
        "@openai/codex": "0.146.0",
      }),
    /refusing to downgrade @openai\/codex from 0\.147\.0 to 0\.146\.0/,
  );
});

test("refuses a numerically lower version", () => {
  const target = targetFor(
    "deploy/railway/worker/Dockerfile",
    "AGENT_NPM_PACKAGES",
  );
  const content = workerDockerfile.replace("2.1.228", "2.1.10");
  assert.throws(
    () =>
      applyTarget(content, target, {
        ...latest,
        "@anthropic-ai/claude-code": "2.1.9",
      }),
    /refusing to downgrade @anthropic-ai\/claude-code/,
  );
});

test("fails when a package has no resolved version", () => {
  const target = targetFor(
    "deploy/railway/worker/Dockerfile",
    "AGENT_NPM_PACKAGES",
  );
  const versions = { ...latest };
  delete versions["@openai/codex"];
  assert.throws(
    () => applyTarget(workerDockerfile, target, versions),
    /no resolved version for @openai\/codex/,
  );
});

test("rejects an invalid resolved version", () => {
  const target = targetFor(
    "deploy/railway/worker/Dockerfile",
    "AGENT_NPM_PACKAGES",
  );
  assert.throws(
    () =>
      applyTarget(workerDockerfile, target, {
        ...latest,
        "@openai/codex": "latest",
      }),
    /invalid version for @openai\/codex/,
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

test("allows a major update for an unlocked package", () => {
  const target = targetFor(
    "deploy/railway/worker/Dockerfile",
    "AGENT_NPM_PACKAGES",
  );
  const result = applyTarget(workerDockerfile, target, {
    ...latest,
    "@anthropic-ai/claude-code": "3.0.0",
  });
  assert.match(result.text, /claude-code@3\.0\.0/);
  assert.deepEqual(result.skipped, []);
});

test("skips a cross-major OpenCode 1 update", () => {
  const target = targetFor(
    "deploy/railway/worker/Dockerfile",
    "AGENT_NPM_PACKAGES",
  );
  const result = applyTarget(workerDockerfile, target, {
    ...latest,
    "opencode-ai": "2.0.0",
  });
  assert.equal(result.changes.length, 2);
  assert.deepEqual(result.skipped, [
    {
      name: "opencode-ai",
      from: "1.19.3",
      to: "2.0.0",
      reason: "major-1 lock",
    },
  ]);
});

test("skips a cross-major OpenCode 2 update", () => {
  const target = targetFor(
    "deploy/railway/hub/Dockerfile",
    "HUB_OPENCODE_V2_PACKAGE",
  );
  const result = applyTarget(hubDockerfile, target, {
    ...latest,
    "@opencode/cli": "3.0.0",
  });
  assert.deepEqual(result.changes, []);
  assert.deepEqual(result.skipped, [
    {
      name: "@opencode/cli",
      from: "2.0.32",
      to: "3.0.0",
      reason: "major-2 lock",
    },
  ]);
});

test("allows a same-major OpenCode bump", () => {
  const workerTarget = targetFor(
    "deploy/railway/worker/Dockerfile",
    "AGENT_NPM_PACKAGES",
  );
  const workerResult = applyTarget(workerDockerfile, workerTarget, {
    ...latest,
    "opencode-ai": "1.20.0",
  });
  assert.match(workerResult.text, /opencode-ai@1\.20\.0/);
  assert.deepEqual(workerResult.skipped, []);

  const hubTarget = targetFor(
    "deploy/railway/hub/Dockerfile",
    "HUB_OPENCODE_V2_PACKAGE",
  );
  const hubResult = applyTarget(hubDockerfile, hubTarget, {
    ...latest,
    "@opencode/cli": "2.1.0",
  });
  assert.match(hubResult.text, /@opencode\/cli@2\.1\.0/);
  assert.deepEqual(hubResult.skipped, []);
});

test("fails when a locked package is pinned outside its major", () => {
  const target = targetFor(
    "deploy/railway/worker/Dockerfile",
    "AGENT_NPM_PACKAGES",
  );
  const content = workerDockerfile.replace(
    "opencode-ai@1.19.3",
    "opencode-ai@2.0.0",
  );
  assert.throws(
    () =>
      applyTarget(content, target, {
        ...latest,
        "opencode-ai": "2.0.1",
      }),
    /opencode-ai is pinned to major 2, outside its major-1 lock/,
  );
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
  assert.deepEqual(first.skipped, []);
  assert.match(
    first.contents.get("deploy/railway/worker/README.md"),
    /claude-code@2\.1\.302/,
  );
  const second = updatePins({ root, versions: latest, write: true });
  assert.deepEqual(second.changes, []);
  assert.deepEqual(second.skipped, []);
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

test("dry run writes the summary but not the Dockerfiles", (t) => {
  const root = makeRoot(t);
  const versionsPath = join(root, "versions.json");
  const summaryPath = join(root, "summary.md");
  writeFileSync(versionsPath, JSON.stringify(latest));
  const code = run({ root, versionsPath, dryRun: true, summaryPath });
  assert.equal(code, 0);
  assert.match(
    readFileSync(join(root, "deploy/railway/worker/Dockerfile"), "utf8"),
    /claude-code@2\.1\.228/,
  );
  assert.match(
    readFileSync(summaryPath, "utf8"),
    /^\| File \| ARG \| Package \| From \| To \|/,
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

test("summaryMarkdown reports a skipped major change", () => {
  const summary = summaryMarkdown([], [
    {
      file: "deploy/railway/worker/Dockerfile",
      arg: "AGENT_NPM_PACKAGES",
      name: "opencode-ai",
      from: "1.19.3",
      to: "2.0.0",
      reason: "major-1 lock",
    },
  ]);
  assert.match(summary, /### Skipped/);
  assert.match(summary, /opencode-ai/);
  assert.match(summary, /major-1 lock/);
});

test("the repository targets all four pinned packages", () => {
  assert.deepEqual(collectPackages(repoRoot), [
    "@anthropic-ai/claude-code",
    "@openai/codex",
    "@opencode/cli",
    "opencode-ai",
  ]);
});

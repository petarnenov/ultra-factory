import assert from "node:assert/strict";
import test from "node:test";

import {
  compareVersions,
  formatSummary,
  parsePins,
  planPins,
  readPinnedValue,
  rewriteDocValue,
  rewritePinnedValue,
} from "./agent-cli-pins.mjs";

const WORKER = [
  'ARG AGENT_NPM_PACKAGES="@anthropic-ai/claude-code@2.1.228 @openai/codex@0.147.0 opencode-ai@1.19.3"',
  "",
].join("\n");
const HUB = [
  'ARG HUB_AGENT_NPM_PACKAGES="@anthropic-ai/claude-code@2.1.228 @openai/codex@0.147.0"',
  'ARG HUB_OPENCODE_V2_PACKAGE="@opencode/cli@2.0.32"',
  "",
].join("\n");
const LATEST = {
  "@anthropic-ai/claude-code": "2.1.302",
  "@openai/codex": "0.163.0",
  "@opencode/cli": "2.0.33",
};
const MAJORS = { "opencode-ai": ["1.19.3", "1.20.0", "2.0.0"] };

function npmName(url) {
  return decodeURIComponent(new URL(url).pathname.slice(1)).replace(/\/latest$/, "");
}

function registry(latest, majors = MAJORS) {
  return async (url) => {
    const name = npmName(url);
    if (url.endsWith("/latest")) {
      return { ok: true, json: async () => ({ version: latest[name] }) };
    }
    const versions = Object.fromEntries(majors[name].map((version) => [version, {}]));
    return { ok: true, json: async () => ({ versions }) };
  };
}

function readFiles(files) {
  return async (file) => files.get(file);
}

const FILES = new Map([
  ["deploy/railway/worker/Dockerfile", WORKER],
  ["deploy/railway/hub/Dockerfile", HUB],
]);

test("parsePins reads scoped and unscoped pins", () => {
  assert.deepEqual(parsePins("@anthropic-ai/claude-code@2.1.302 opencode-ai@1.19.3"), [
    {
      token: "@anthropic-ai/claude-code@2.1.302",
      name: "@anthropic-ai/claude-code",
      version: "2.1.302",
    },
    { token: "opencode-ai@1.19.3", name: "opencode-ai", version: "1.19.3" },
  ]);
  assert.throws(() => parsePins("claude-code"), /invalid pinned package/);
});

test("readPinnedValue rejects a missing ARG", () => {
  assert.throws(() => readPinnedValue(WORKER, "NOPE"), /missing single-line ARG NOPE/);
});

test("rewritePinnedValue changes only the requested pin", () => {
  const next = rewritePinnedValue(WORKER, "AGENT_NPM_PACKAGES", [
    { name: "@anthropic-ai/claude-code", from: "2.1.228", to: "2.1.302" },
  ]);
  assert.match(next, /@anthropic-ai\/claude-code@2\.1\.302/);
  assert.match(next, /@openai\/codex@0\.147\.0/);
  assert.match(next, /opencode-ai@1\.19\.3/);
  assert.equal(
    rewritePinnedValue(WORKER, "AGENT_NPM_PACKAGES", [
      { name: "opencode-ai", from: "9.9.9", to: "9.9.10" },
    ]),
    WORKER,
  );
});

test("compareVersions compares numerically", () => {
  assert.equal(compareVersions("1.10.0", "1.9.0"), 1);
  assert.equal(compareVersions("1.19.3", "1.19.3"), 0);
  assert.equal(compareVersions("0.147.0", "0.163.0"), -1);
});

test("planPins updates the worker and hub pins together", async () => {
  const { updates } = await planPins({
    readFileImpl: readFiles(FILES),
    fetchImpl: registry(LATEST),
  });
  assert.deepEqual(updates.map((item) => `${item.file}:${item.name}@${item.to}`), [
    "deploy/railway/worker/Dockerfile:@anthropic-ai/claude-code@2.1.302",
    "deploy/railway/worker/Dockerfile:@openai/codex@0.163.0",
    "deploy/railway/worker/Dockerfile:opencode-ai@1.20.0",
    "deploy/railway/hub/Dockerfile:@anthropic-ai/claude-code@2.1.302",
    "deploy/railway/hub/Dockerfile:@openai/codex@0.163.0",
    "deploy/railway/hub/Dockerfile:@opencode/cli@2.0.33",
  ]);
  assert.equal(updates[0].from, "2.1.228");
  const worker = updates.filter((item) => item.file.endsWith("worker/Dockerfile"));
  assert.equal(
    rewritePinnedValue(WORKER, "AGENT_NPM_PACKAGES", worker),
    [
      'ARG AGENT_NPM_PACKAGES="@anthropic-ai/claude-code@2.1.302 @openai/codex@0.163.0 opencode-ai@1.20.0"',
      "",
    ].join("\n"),
  );
});

test("planPins keeps worker OpenCode on major 1", async () => {
  const { updates } = await planPins({
    readFileImpl: readFiles(FILES),
    fetchImpl: registry(LATEST),
  });
  const opencode = updates.find((item) => item.name === "opencode-ai");
  assert.equal(opencode.to, "1.20.0");
});

test("planPins does not downgrade a pin", async () => {
  const { updates, skipped } = await planPins({
    readFileImpl: readFiles(FILES),
    fetchImpl: registry({ ...LATEST, "@openai/codex": "0.140.0" }),
  });
  assert.ok(!updates.some((item) => item.name === "@openai/codex"));
  assert.ok(skipped.some((item) => item.name === "@openai/codex"));
  assert.equal(skipped.find((item) => item.name === "@openai/codex").reason, "npm latest is older");
});

test("planPins surfaces a registry failure", async () => {
  const fetchImpl = async () => ({ ok: false, status: 503, json: async () => ({}) });
  await assert.rejects(
    planPins({ readFileImpl: readFiles(FILES), fetchImpl }),
    /npm registry 503/,
  );
});

test("formatSummary renders a markdown table", () => {
  const summary = formatSummary([
    { file: "Dockerfile", arg: "PINS", name: "pkg", from: "1.0.0", to: "1.0.1" },
  ]);
  assert.match(summary, /\| `Dockerfile` \| `PINS` \| `pkg` \| 1\.0\.0 \| 1\.0\.1 \|/);
});

test("rewriteDocValue updates the documented default", () => {
  const row = "| `AGENT_NPM_PACKAGES` | `old` | Pinned installs. |";
  assert.equal(
    rewriteDocValue(row, "AGENT_NPM_PACKAGES", "new"),
    "| `AGENT_NPM_PACKAGES` | `new` | Pinned installs. |",
  );
  assert.throws(() => rewriteDocValue(row, "NOPE", "new"), /missing docs row for NOPE/);
});

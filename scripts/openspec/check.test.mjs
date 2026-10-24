import assert from "node:assert/strict";
import test from "node:test";
import path from "node:path";
import { runOpenSpecCheck } from "./check.mjs";

const root = path.resolve(import.meta.dirname, "../..");

test("unsupported Node reports setup guidance before checking or spawning OpenSpec", async () => {
	const errors = [];
	let existenceChecks = 0;
	let spawnCalls = 0;
	const status = await runOpenSpecCheck({
		root,
		nodeVersion: "22.14.0",
		executableExists: () => {
			existenceChecks += 1;
			return true;
		},
		spawn: () => {
			spawnCalls += 1;
		},
		error: (message) => errors.push(message),
	});

	assert.equal(status, 1);
	assert.deepEqual(errors, [
		"OpenSpec check requires Node 24 (found 22.14.0). Switch to Node 24, then rerun `npm run openspec:check` from the repository root.",
	]);
	assert.equal(existenceChecks, 0);
	assert.equal(spawnCalls, 0);
});

test("missing root-pinned executable points to npm ci without spawning OpenSpec", async () => {
	const errors = [];
	const paths = [];
	let spawnCalls = 0;
	const status = await runOpenSpecCheck({
		root,
		nodeVersion: "24.21.0",
		executableExists: (file) => {
			paths.push(file);
			return false;
		},
		spawn: () => {
			spawnCalls += 1;
		},
		error: (message) => errors.push(message),
	});

	assert.equal(status, 1);
	assert.deepEqual(paths, [path.join(root, "node_modules", ".bin", "openspec")]);
	assert.deepEqual(errors, [
		"OpenSpec CLI is missing at `node_modules/.bin/openspec`. Run `npm ci` from the repository root, then rerun `npm run openspec:check`.",
	]);
	assert.equal(spawnCalls, 0);
});

test("supported setup preserves the pinned sample and malformed/repaired validation sequence", async () => {
	const calls = [];
	const output = [];
	const status = await runOpenSpecCheck({
		root,
		nodeVersion: "24.21.0",
		executableExists: () => true,
		spawn: (_command, args, options) => {
			calls.push({ args, cwd: options.cwd });
			if (args[0].endsWith("openspec") && args[1] === "--version") {
				return { status: 0, stdout: "1.14.3\n", stderr: "" };
			}
			if (args[2] === "malformed-change") {
				return { status: 1, stdout: "Requirement is missing a scenario", stderr: "" };
			}
			return { status: 0, stdout: "", stderr: "" };
		},
		log: (message) => output.push(message),
		error: (message) => assert.fail(message),
	});

	assert.equal(status, 0);
	assert.deepEqual(
		calls.map(({ args }) => args.slice(1)),
		[
			["--version"],
			["validate", "sdd-01-openspec-bootstrap", "--strict", "--no-interactive"],
			["validate", "malformed-change", "--strict", "--no-interactive"],
			["validate", "control-change", "--strict", "--no-interactive"],
		],
	);
	assert.deepEqual(output, [
		"OpenSpec 1.14.3 validated the real bootstrap change.",
		"OpenSpec rejected the malformed fixture and accepted its repaired control.",
	]);
});

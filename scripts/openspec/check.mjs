import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { cp, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const changeId = "sdd-01-openspec-bootstrap";

export async function runOpenSpecCheck({
	root = repoRoot,
	nodeVersion = process.versions.node,
	executableExists = existsSync,
	spawn = spawnSync,
	log = console.log,
	error = console.error,
} = {}) {
	const openspecBin = path.join(root, "node_modules", ".bin", "openspec");
	if (nodeVersion.split(".")[0] !== "24") {
		error(
			`OpenSpec check requires Node 24 (found ${nodeVersion}). Switch to Node 24, then rerun ` +
				"`npm run openspec:check` from the repository root.",
		);
		return 1;
	}
	if (!executableExists(openspecBin)) {
		error(
			"OpenSpec CLI is missing at `node_modules/.bin/openspec`. Run `npm ci` from the " +
				"repository root, then rerun `npm run openspec:check`.",
		);
		return 1;
	}

	function validate(cwd, name) {
		return spawn(
			process.execPath,
			[openspecBin, "validate", name, "--strict", "--no-interactive"],
			{
				cwd,
				encoding: "utf8",
				env: { ...process.env, OPENSPEC_TELEMETRY: "0" },
				maxBuffer: 2 * 1024 * 1024,
			},
		);
	}

	const version = spawn(process.execPath, [openspecBin, "--version"], {
		cwd: root,
		encoding: "utf8",
		maxBuffer: 1024 * 1024,
	});
	assert.equal(version.status, 0, version.stderr);
	assert.equal(version.stdout.trim(), "1.14.3", "OpenSpec version must match the committed pin");

	const sample = validate(root, changeId);
	assert.equal(sample.status, 0, `${sample.stdout}\n${sample.stderr}`);

	const tempRoot = await mkdtemp(path.join(os.tmpdir(), "ao-openspec-check-"));
	try {
		const changesDir = path.join(tempRoot, "openspec", "changes");
		const malformedDir = path.join(changesDir, "malformed-change");
		const controlDir = path.join(changesDir, "control-change");
		const fixturePath = path.join(root, "scripts", "openspec", "fixtures", "malformed-change");
		await cp(path.join(root, "openspec", "config.yaml"), path.join(tempRoot, "openspec", "config.yaml"));
		await cp(fixturePath, malformedDir, { recursive: true });
		await cp(fixturePath, controlDir, { recursive: true });

		const malformed = validate(tempRoot, "malformed-change");
		assert.notEqual(malformed.status, 0, "Malformed requirement unexpectedly passed validation");
		assert.match(
			`${malformed.stdout}\n${malformed.stderr}`,
			/scenario/i,
			"Malformed fixture failed without reporting the missing-scenario validation error",
		);

		const controlSpec = path.join(controlDir, "specs", "malformed-fixture", "spec.md");
		const contents = await readFile(controlSpec, "utf8");
		await writeFile(
			controlSpec,
			`${contents}\n#### Scenario: Requirement has an observable outcome\n\n- **WHEN** the validator checks the requirement\n- **THEN** it finds a complete scenario\n`,
		);
		const control = validate(tempRoot, "control-change");
		assert.equal(control.status, 0, `${control.stdout}\n${control.stderr}`);

		log("OpenSpec 1.14.3 validated the real bootstrap change.");
		log("OpenSpec rejected the malformed fixture and accepted its repaired control.");
	} finally {
		await rm(tempRoot, { recursive: true, force: true });
	}
	return 0;
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
	process.exitCode = await runOpenSpecCheck();
}

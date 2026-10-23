import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { cp, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const openspecBin = path.join(repoRoot, "node_modules", ".bin", "openspec");
const configPath = path.join(repoRoot, "openspec", "config.yaml");
const changeId = "sdd-01-openspec-bootstrap";
const fixturePath = path.join(repoRoot, "scripts", "openspec", "fixtures", "malformed-change");

function validate(cwd, name) {
	return spawnSync(
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

assert.equal(process.versions.node.split(".")[0], "24", "OpenSpec checks require Node 24");

const version = spawnSync(process.execPath, [openspecBin, "--version"], {
	cwd: repoRoot,
	encoding: "utf8",
	maxBuffer: 1024 * 1024,
});
assert.equal(version.status, 0, version.stderr);
assert.equal(version.stdout.trim(), "1.14.3", "OpenSpec version must match the committed pin");

const sample = validate(repoRoot, changeId);
assert.equal(sample.status, 0, `${sample.stdout}\n${sample.stderr}`);

const tempRoot = await mkdtemp(path.join(os.tmpdir(), "ao-openspec-check-"));
try {
	const changesDir = path.join(tempRoot, "openspec", "changes");
	const malformedDir = path.join(changesDir, "malformed-change");
	const controlDir = path.join(changesDir, "control-change");
	await cp(configPath, path.join(tempRoot, "openspec", "config.yaml"));
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

	console.log("OpenSpec 1.14.3 validated the real bootstrap change.");
	console.log("OpenSpec rejected the malformed fixture and accepted its repaired control.");
} finally {
	await rm(tempRoot, { recursive: true, force: true });
}

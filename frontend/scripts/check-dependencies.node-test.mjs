import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdtempSync, mkdirSync, readFileSync, realpathSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const frontendDir = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(frontendDir, "..");
const rootDir = path.resolve(frontendRoot, "..");
const checkScript = path.join(frontendDir, "check-dependencies.sh");
const sharedRule = path.join(rootDir, "scripts/dependency-freshness.sh");

function createFixture() {
	const dir = mkdtempSync(path.join(os.tmpdir(), "ao-dependency-freshness-"));
	mkdirSync(path.join(dir, "node_modules"));
	writeFileSync(path.join(dir, "package-lock.json"), "{}");
	return dir;
}

function createDevFixture(t) {
	const dir = mkdtempSync(path.join(os.tmpdir(), "ao-dev-freshness-"));
	const frontend = path.join(dir, "frontend");
	const scripts = path.join(frontend, "scripts");
	const sharedScripts = path.join(dir, "scripts");
	const bin = path.join(dir, "bin");
	const calls = path.join(dir, "calls.log");
	mkdirSync(scripts, { recursive: true });
	mkdirSync(sharedScripts);
	mkdirSync(bin);

	const manifest = JSON.parse(readFileSync(path.join(frontendRoot, "package.json"), "utf8"));
	writeFileSync(path.join(frontend, "package.json"), JSON.stringify({
		name: "dev-freshness-fixture",
		private: true,
		scripts: { predev: manifest.scripts.predev, dev: manifest.scripts.dev },
	}));
	writeFileSync(path.join(scripts, "check-dependencies.sh"), readFileSync(checkScript));
	writeFileSync(path.join(sharedScripts, "dependency-freshness.sh"), readFileSync(sharedRule));
	for (const command of ["npm", "node", "electron-forge"]) {
		const stub = path.join(bin, command);
		writeFileSync(stub, '#!/usr/bin/env bash\nprintf "%s\\n" "$0 $*" >> "$CALL_LOG"\n');
		chmodSync(stub, 0o755);
	}
	t.after(() => rmSync(dir, { recursive: true, force: true }));

	return { dir, frontend, calls, bin };
}

function freshness(dir) {
	return execFileSync("bash", ["-c", `. '${sharedRule}' && dependency_freshness "$1"`, "bash", dir], {
		encoding: "utf8",
	}).trim();
}

test("dependency freshness detects missing, stale, and fresh installs", (t) => {
	const dir = createFixture();
	t.after(() => rmSync(dir, { recursive: true, force: true }));

	assert.equal(freshness(path.join(dir, "absent")), "missing");
	assert.equal(freshness(dir), "stale");

	const marker = path.join(dir, "node_modules/.package-lock.json");
	writeFileSync(marker, "{}");
	const older = new Date(Date.now() - 10_000);
	utimesSync(path.join(dir, "package-lock.json"), older, older);
	assert.equal(freshness(dir), "fresh");

	const newer = new Date(Date.now() + 10_000);
	utimesSync(path.join(dir, "package-lock.json"), newer, newer);
	assert.equal(freshness(dir), "stale");
});

test("predev blocks stale installs before daemon, runtime, and Electron commands", (t) => {
	const fixture = createDevFixture(t);
	const env = {
		...process.env,
		PATH: `${fixture.bin}:${process.env.PATH}`,
		CALL_LOG: fixture.calls,
	};
	const npm = realpathSync(execFileSync("which", ["npm"], { encoding: "utf8" }).trim());
	const lockfile = path.join(fixture.frontend, "package-lock.json");
	const nodeModules = path.join(fixture.frontend, "node_modules");
	mkdirSync(nodeModules);
	writeFileSync(lockfile, "{}");
	const staleResult = spawnSync(process.execPath, [npm, "--prefix", fixture.frontend, "run", "dev"], {
		cwd: fixture.dir,
		env,
		encoding: "utf8",
	});
	assert.notEqual(staleResult.status, 0, `${staleResult.stdout}\n${staleResult.stderr}`);
	assert.match(staleResult.stderr, /frontend dependencies are out of date\. Run: make setup/);
	assert.equal(existsSync(fixture.calls), false);

	const marker = path.join(nodeModules, ".package-lock.json");
	writeFileSync(marker, "{}");
	const older = new Date(Date.now() - 10_000);
	utimesSync(lockfile, older, older);
	const freshResult = spawnSync(process.execPath, [npm, "--prefix", fixture.frontend, "run", "dev"], {
		cwd: fixture.dir,
		env,
		encoding: "utf8",
	});
	assert.equal(freshResult.status, 0, freshResult.stderr);
	const calls = readFileSync(fixture.calls, "utf8");
	assert.match(calls, /npm run build:daemon -- --dev/);
	assert.match(calls, /node \.\/scripts\/ensure-browser-runtime\.mjs/);
	assert.match(calls, /npm run build:acp-runtime/);
	assert.match(calls, /electron-forge start/);
});

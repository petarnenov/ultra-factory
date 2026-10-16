import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// The repo has no ESLint, so this source scan is the lint rule: renderer code
// reaches the Electron preload only through ./bridge (aoBridge, with browser
// stubs) or ./electron-bridge (the live window.ao read). Anything else would
// crash or silently misbehave in the web build, which has no window.ao.
const rendererDirectory = path.resolve(process.cwd(), "src/renderer");
const allowedFiles = new Set(["lib/bridge.ts", "lib/electron-bridge.ts"]);
const directAccess = /\bwindow\.ao\b/;

function rendererSources(directory: string): string[] {
	return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
		const fullPath = path.join(directory, entry.name);
		if (entry.isDirectory()) return entry.name === "test" ? [] : rendererSources(fullPath);
		if (!/\.tsx?$/.test(entry.name) || /\.test\.tsx?$/.test(entry.name)) return [];
		return [fullPath];
	});
}

describe("Electron bridge boundary", () => {
	it("keeps direct window.ao access inside lib/bridge.ts and lib/electron-bridge.ts", () => {
		const offenders = rendererSources(rendererDirectory).flatMap((file) => {
			const relative = path.relative(rendererDirectory, file).split(path.sep).join("/");
			if (allowedFiles.has(relative)) return [];
			return readFileSync(file, "utf8")
				.split("\n")
				.flatMap((line, index) => (directAccess.test(line) ? [`${relative}:${index + 1}: ${line.trim()}`] : []));
		});

		expect(offenders).toEqual([]);
	});
});

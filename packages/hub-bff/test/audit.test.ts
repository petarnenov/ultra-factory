import { chmodSync, mkdtempSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { openAuditLog } from "../src/audit.ts";

let dir = "";
afterEach(() => {
	if (dir) rmSync(dir, { recursive: true, force: true });
});

describe("volume audit log", () => {
	it("appends sanitized JSONL entries with private permissions and returns the newest limit", async () => {
		dir = mkdtempSync(path.join(tmpdir(), "hub-audit-"));
		const file = path.join(dir, "hub", "audit.jsonl");
		const audit = await openAuditLog(file);
		await Promise.all([
			audit.record("login.success"),
			audit.record("worker.create", "worker-1"),
			audit.record("worker.force_kill", "worker-1"),
		]);
		expect(statSync(file).mode & 0o777).toBe(0o600);
		expect((await audit.list(2)).map((entry) => entry.event)).toEqual(["worker.create", "worker.force_kill"]);
		const raw = readFileSync(file, "utf8");
		expect(raw.trim().split("\n")).toHaveLength(3);
		expect(raw).not.toMatch(/password|token|secret|cookie/i);
	});

	it("refuses a broadly readable existing file and symbolic links", async () => {
		dir = mkdtempSync(path.join(tmpdir(), "hub-audit-"));
		const broad = path.join(dir, "broad.jsonl");
		writeFileSync(broad, "", { mode: 0o600 });
		chmodSync(broad, 0o644);
		await expect(openAuditLog(broad)).rejects.toThrow(/group or others/);
		const target = path.join(dir, "target.jsonl");
		writeFileSync(target, "", { mode: 0o600 });
		const link = path.join(dir, "link.jsonl");
		symlinkSync(target, link);
		await expect(openAuditLog(link)).rejects.toThrow(/symbolic link/);
	});
});

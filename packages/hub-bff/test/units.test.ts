import { randomBytes } from "node:crypto";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { loadConfig } from "../src/config.ts";
import { jsonLogger } from "../src/log.ts";
import { hashPassword, parsePasswordHash, verifyPassword } from "../src/password.ts";

const TEST_COST = { ln: 10, r: 8, p: 1 };

describe("password hashes", () => {
	it("round-trips a generated password", async () => {
		const password = randomBytes(12).toString("base64url");
		const encoded = await hashPassword(password, TEST_COST);
		expect(encoded).toMatch(/^\$scrypt\$ln=10,r=8,p=1\$[A-Za-z0-9_-]+\$[A-Za-z0-9_-]+$/);
		const parsed = parsePasswordHash(encoded);
		expect(await verifyPassword(password, parsed)).toBe(true);
		expect(await verifyPassword(`${password}!`, parsed)).toBe(false);
	});

	it("salts every hash", async () => {
		expect(await hashPassword("same", TEST_COST)).not.toBe(await hashPassword("same", TEST_COST));
	});

	it.each(["", "plain", "$argon2id$v=19$m=65536,t=3,p=4$abc$def", "$scrypt$ln=99,r=8,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaA"])(
		"rejects malformed hash %j",
		(encoded) => {
			expect(() => parsePasswordHash(encoded)).toThrow();
		},
	);
});

describe("loadConfig", () => {
	function withSpa<T>(fn: (dir: string) => Promise<T>): Promise<T> {
		const dir = mkdtempSync(path.join(tmpdir(), "hub-bff-cfg-"));
		writeFileSync(path.join(dir, "index.html"), "x");
		return fn(dir).finally(() => rmSync(dir, { recursive: true, force: true }));
	}

	it("requires the SPA dir and a password", async () => {
		await expect(loadConfig({})).rejects.toThrow(/AO_HUB_SPA_DIR/);
		await withSpa((dir) => expect(loadConfig({ AO_HUB_SPA_DIR: dir })).rejects.toThrow(/AO_WEB_PASSWORD_HASH/));
	});

	it("accepts a hash, and hashes a plaintext password without exposing it", async () => {
		const hash = await hashPassword("pw", TEST_COST);
		await withSpa(async (dir) => {
			const fromHash = await loadConfig({ AO_HUB_SPA_DIR: dir, AO_WEB_PASSWORD_HASH: hash, PORT: "9000" });
			expect(fromHash).toMatchObject({ port: 9000, passwordHash: hash, plaintextPassword: false });
			const plaintext = randomBytes(12).toString("base64url");
			const fromPlain = await loadConfig({ AO_HUB_SPA_DIR: dir, AO_WEB_PASSWORD: plaintext });
			expect(fromPlain.passwordHash).not.toContain(plaintext);
			expect(await verifyPassword(plaintext, parsePasswordHash(fromPlain.passwordHash))).toBe(true);
			await expect(loadConfig({ AO_HUB_SPA_DIR: dir, AO_WEB_PASSWORD: "a", AO_WEB_PASSWORD_HASH: hash })).rejects.toThrow(/only one/);
		});
	});

	it("validates AO_WEB_ORIGIN", async () => {
		const hash = await hashPassword("pw", TEST_COST);
		await withSpa(async (dir) => {
			const base = { AO_HUB_SPA_DIR: dir, AO_WEB_PASSWORD_HASH: hash };
			expect((await loadConfig({ ...base, AO_WEB_ORIGIN: "https://hub.example.com" })).publicOrigin).toBe("https://hub.example.com");
			await expect(loadConfig({ ...base, AO_WEB_ORIGIN: "https://hub.example.com/app" })).rejects.toThrow(/AO_WEB_ORIGIN/);
		});
	});

	it("defaults the worker limit to five and validates overrides", async () => {
		const hash = await hashPassword("pw", TEST_COST);
		await withSpa(async (dir) => {
			const base = { AO_HUB_SPA_DIR: dir, AO_WEB_PASSWORD_HASH: hash };
			expect((await loadConfig(base)).maxWorkers).toBe(5);
			expect((await loadConfig({ ...base, AO_HUB_MAX_WORKERS: "8" })).maxWorkers).toBe(8);
			await expect(loadConfig({ ...base, AO_HUB_MAX_WORKERS: "0" })).rejects.toThrow(/AO_HUB_MAX_WORKERS/);
		});
	});
});

describe("jsonLogger", () => {
	it("writes one JSON object per line", () => {
		const lines: string[] = [];
		jsonLogger((line) => lines.push(line))("info", "hello", { status: 200 });
		expect(lines).toHaveLength(1);
		expect(lines[0].endsWith("\n")).toBe(true);
		expect(JSON.parse(lines[0])).toMatchObject({ level: "info", msg: "hello", status: 200 });
	});
});

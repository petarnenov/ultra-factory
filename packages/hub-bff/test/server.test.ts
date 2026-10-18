import { randomBytes } from "node:crypto";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import type { Server } from "node:http";
import { tmpdir } from "node:os";
import path from "node:path";
import request from "supertest";
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { LogFields } from "../src/log.ts";
import type { AuditEntry, AuditLog, AuditEvent } from "../src/audit.ts";
import { hashPassword } from "../src/password.ts";
import { LOGIN_ATTEMPTS_PER_MINUTE, MAX_CONCURRENT_PASSWORD_CHECKS, SESSION_COOKIE, createBff } from "../src/server.ts";
import { SESSION_IDLE_MS } from "../src/sessions.ts";

// A cheap scrypt cost keeps the suite fast; production uses DEFAULT_COST.
const TEST_COST = { ln: 10, r: 8, p: 1 };
const HOST = "hub.test";
const ORIGIN = `http://${HOST}`;

let spaDir: string;
let password: string;
let passwordHash: string;

beforeAll(async () => {
	spaDir = mkdtempSync(path.join(tmpdir(), "hub-bff-spa-"));
	mkdirSync(path.join(spaDir, "assets"));
	writeFileSync(path.join(spaDir, "index.html"), "<!doctype html><title>spa</title>");
	writeFileSync(path.join(spaDir, "assets", "app-abc123.js"), "console.log('spa')");
	password = randomBytes(18).toString("base64url");
	passwordHash = await hashPassword(password, TEST_COST);
});

afterAll(() => rmSync(spaDir, { recursive: true, force: true }));

type Harness = { server: Server; logs: string[]; auditEntries: AuditEntry[]; advance: (ms: number) => void };

function harness(): Harness {
	const logs: string[] = [];
	const auditEntries: AuditEntry[] = [];
	const audit: AuditLog = {
		async record(event: AuditEvent, subjectId?: string) { auditEntries.push({ ts: new Date().toISOString(), event, ...(subjectId ? { subjectId } : {}) }); },
		async list(limit: number) { return auditEntries.slice(-limit); },
	};
	let now = Date.now();
	const log = (level: string, msg: string, fields?: LogFields) => logs.push(JSON.stringify({ level, msg, ...fields }));
	const server = createBff({ spaDir, passwordHash, log, audit, now: () => now });
	return { server, logs, auditEntries, advance: (ms) => (now += ms) };
}

function login(server: Server, body: unknown, headers: Record<string, string> = {}) {
	return request(server)
		.post("/login")
		.set("Host", HOST)
		.set("Origin", ORIGIN)
		.set("Sec-Fetch-Site", "same-origin")
		.set(headers)
		.send(body as object);
}

async function sessionCookie(server: Server): Promise<string> {
	const res = await login(server, { password });
	expect(res.status).toBe(204);
	const cookie = (res.headers["set-cookie"] as unknown as string[])[0];
	return cookie.split(";")[0];
}

describe("hub bff", () => {
	let h: Harness;
	beforeEach(() => {
		h = harness();
	});

	it("serves /healthz without a session", async () => {
		const res = await request(h.server).get("/healthz");
		expect(res.status).toBe(200);
		expect(res.body).toEqual({ ok: true });
	});

	it("serves the login page and its assets without a session", async () => {
		const page = await request(h.server).get("/login");
		expect(page.status).toBe(200);
		expect(page.headers["content-type"]).toMatch(/text\/html/);
		expect(page.headers["content-security-policy"]).toContain("default-src 'none'");
		expect((await request(h.server).get("/_bff/login.js")).status).toBe(200);
		expect((await request(h.server).get("/_bff/login.css")).status).toBe(200);
	});

	it.each(["/", "/index.html", "/assets/app-abc123.js", "/sessions/abc", "/api/v1/sessions", "/mux"])(
		"answers 401 for %s without a cookie",
		async (url) => {
			const res = await request(h.server).get(url).set("Accept", "application/json");
			expect(res.status).toBe(401);
			expect(res.body).toEqual({ error: "unauthorized" });
		},
	);

	it("redirects an unauthenticated page navigation to /login", async () => {
		const res = await request(h.server).get("/").set("Accept", "text/html").set("Sec-Fetch-Mode", "navigate");
		expect(res.status).toBe(302);
		expect(res.headers.location).toBe("/login");
	});

	it("rejects an unknown session cookie", async () => {
		const res = await request(h.server).get("/").set("Cookie", `${SESSION_COOKIE}=${randomBytes(32).toString("base64url")}`);
		expect(res.status).toBe(401);
	});

	it("logs in with the right password and sets a locked-down cookie", async () => {
		const res = await login(h.server, { password });
		expect(res.status).toBe(204);
		const cookie = (res.headers["set-cookie"] as unknown as string[])[0];
		expect(cookie).toMatch(new RegExp(`^${SESSION_COOKIE}=[A-Za-z0-9_-]{43};`));
		for (const flag of ["HttpOnly", "Secure", "SameSite=Strict", "Path=/"]) expect(cookie).toContain(`; ${flag}`);
	});

	it("serves the SPA with a session, including client-side routes", async () => {
		const cookie = await sessionCookie(h.server);
		const index = await request(h.server).get("/").set("Cookie", cookie);
		expect(index.status).toBe(200);
		expect(index.text).toContain("<title>spa</title>");
		expect(index.headers["cache-control"]).toBe("no-cache");
		const asset = await request(h.server).get("/assets/app-abc123.js").set("Cookie", cookie);
		expect(asset.status).toBe(200);
		expect(asset.headers["content-type"]).toMatch(/javascript/);
		expect(asset.headers["cache-control"]).toContain("immutable");
		expect((await request(h.server).get("/sessions/abc").set("Cookie", cookie)).text).toContain("<title>spa</title>");
		expect((await request(h.server).get("/assets/missing.js").set("Cookie", cookie)).status).toBe(404);
	});

	it("does not serve files outside the SPA directory", async () => {
		const cookie = await sessionCookie(h.server);
		const res = await request(h.server).get("/assets%2F..%2F..%2Fetc%2Fpasswd").set("Cookie", cookie);
		expect(res.status).toBe(404);
	});

	it("redirects /login to the SPA when already signed in", async () => {
		const cookie = await sessionCookie(h.server);
		const res = await request(h.server).get("/login").set("Cookie", cookie);
		expect(res.status).toBe(302);
		expect(res.headers.location).toBe("/");
	});

	it("rejects a wrong password", async () => {
		const res = await login(h.server, { password: `${password}x` });
		expect(res.status).toBe(401);
		expect(res.body).toEqual({ error: "invalid_credentials" });
		expect(res.headers["set-cookie"]).toBeUndefined();
		expect(h.auditEntries.map((entry) => entry.event)).toContain("login.failure");
	});

	it("serves recent audit entries only to an authenticated session and validates limit", async () => {
		await login(h.server, { password: "wrong" });
		const cookie = await sessionCookie(h.server);
		const denied = await request(h.server).get("/hub/audit");
		expect(denied.status).toBe(401);
		const recent = await request(h.server).get("/hub/audit?limit=1").set("Cookie", cookie);
		expect(recent.status).toBe(200);
		expect(recent.body.entries).toHaveLength(1);
		expect(recent.body.entries[0].event).toBe("login.success");
		expect((await request(h.server).get("/hub/audit?limit=1001").set("Cookie", cookie)).status).toBe(400);
		expect((await request(h.server).get("/hub/audit?limit=wat").set("Cookie", cookie)).status).toBe(400);
	});

	it.each([
		["a non-JSON body", "password=x", "application/x-www-form-urlencoded", 415],
		["malformed JSON", "{", "application/json", 400],
		["a missing password", "{}", "application/json", 401],
		["an oversized body", JSON.stringify({ password: "x".repeat(10_000) }), "application/json", 400],
	])("rejects %s", async (_name, body, type, status) => {
		const res = await login(h.server, body, { "Content-Type": type });
		expect(res.status).toBe(status);
	});

	it("expires a session after the idle timeout and slides it on use", async () => {
		const cookie = await sessionCookie(h.server);
		h.advance(SESSION_IDLE_MS - 1000);
		expect((await request(h.server).get("/").set("Cookie", cookie)).status).toBe(200);
		h.advance(SESSION_IDLE_MS - 1000);
		expect((await request(h.server).get("/").set("Cookie", cookie)).status).toBe(200);
		h.advance(SESSION_IDLE_MS + 1);
		expect((await request(h.server).get("/").set("Cookie", cookie)).status).toBe(401);
	});

	it("logs out and invalidates the session", async () => {
		const cookie = await sessionCookie(h.server);
		const res = await request(h.server).post("/logout").set("Host", HOST).set("Origin", ORIGIN).set("Cookie", cookie);
		expect(res.status).toBe(204);
		expect((res.headers["set-cookie"] as unknown as string[])[0]).toContain("Max-Age=0");
		expect((await request(h.server).get("/").set("Cookie", cookie)).status).toBe(401);
	});

	describe("login rate limit", () => {
		it(`allows ${LOGIN_ATTEMPTS_PER_MINUTE} attempts per minute per client, then 429`, async () => {
			const xff = { "X-Forwarded-For": "198.51.100.7" };
			for (let i = 0; i < LOGIN_ATTEMPTS_PER_MINUTE; i++) {
				expect((await login(h.server, { password: "wrong" }, xff)).status).toBe(401);
			}
			const limited = await login(h.server, { password }, xff);
			expect(limited.status).toBe(429);
			expect(Number(limited.headers["retry-after"])).toBeGreaterThan(0);
			// Another client behind the same edge is unaffected.
			expect((await login(h.server, { password }, { "X-Forwarded-For": "198.51.100.8" })).status).toBe(204);
			// The window resets after a minute.
			h.advance(60_000);
			expect((await login(h.server, { password }, xff)).status).toBe(204);
		});

		it("keys on the rightmost X-Forwarded-For entry, so a spoofed leftmost entry does not evade it", async () => {
			for (let i = 0; i < LOGIN_ATTEMPTS_PER_MINUTE; i++) {
				const spoofed = { "X-Forwarded-For": `203.0.113.${i}, 198.51.100.9` };
				expect((await login(h.server, { password: "wrong" }, spoofed)).status).toBe(401);
			}
			expect((await login(h.server, { password }, { "X-Forwarded-For": "203.0.113.99, 198.51.100.9" })).status).toBe(429);
		});
	});

	describe("concurrent password checks", () => {
		it(`refuses with 503 above ${MAX_CONCURRENT_PASSWORD_CHECKS} checks in flight without calling scrypt`, async () => {
			const pending: Array<(ok: boolean) => void> = [];
			let calls = 0;
			const verify = () => {
				calls += 1;
				return new Promise<boolean>((resolve) => pending.push(resolve));
			};
			const server = createBff({ spaDir, passwordHash, log: () => {}, verify });
			await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
			try {
				// Distinct clients, so the per-client throttle stays out of the way.
				const inFlight = Array.from({ length: MAX_CONCURRENT_PASSWORD_CHECKS }, (_, i) =>
					login(server, { password }, { "X-Forwarded-For": `198.51.100.${20 + i}` }).then((r) => r),
				);
				await vi.waitFor(() => expect(calls).toBe(MAX_CONCURRENT_PASSWORD_CHECKS));
				const refused = await login(server, { password }, { "X-Forwarded-For": "198.51.100.99" });
				expect(refused.status).toBe(503);
				expect(refused.body).toEqual({ error: "busy" });
				expect(refused.headers["retry-after"]).toBe("1");
				expect(calls).toBe(MAX_CONCURRENT_PASSWORD_CHECKS);
				// A freed slot accepts checks again.
				for (const resolve of pending.splice(0)) resolve(true);
				for (const res of await Promise.all(inFlight)) expect(res.status).toBe(204);
				const next = login(server, { password }, { "X-Forwarded-For": "198.51.100.99" }).then((r) => r);
				await vi.waitFor(() => expect(calls).toBe(MAX_CONCURRENT_PASSWORD_CHECKS + 1));
				pending.splice(0).forEach((resolve) => resolve(false));
				expect((await next).status).toBe(401);
			} finally {
				server.close();
			}
		});
	});

	describe("CSRF", () => {
		it("refuses login from a foreign Origin", async () => {
			const res = await login(h.server, { password }, { Origin: "https://evil.example" });
			expect(res.status).toBe(403);
			expect(res.headers["set-cookie"]).toBeUndefined();
		});

		it("refuses a cross-site Sec-Fetch-Site even with a matching Origin", async () => {
			expect((await login(h.server, { password }, { "Sec-Fetch-Site": "cross-site" })).status).toBe(403);
		});

		it("refuses a state-changing request without an Origin, even with a valid session", async () => {
			const cookie = await sessionCookie(h.server);
			const res = await request(h.server).post("/logout").set("Host", HOST).set("Cookie", cookie);
			expect(res.status).toBe(403);
			expect((await request(h.server).get("/").set("Cookie", cookie)).status).toBe(200);
		});

		it("refuses a state-changing request from a foreign Origin with a valid session", async () => {
			const cookie = await sessionCookie(h.server);
			const res = await request(h.server)
				.post("/api/v1/sessions")
				.set("Host", HOST)
				.set("Origin", "https://evil.example")
				.set("Cookie", cookie)
				.send({});
			expect(res.status).toBe(403);
		});

		it("honors a configured public origin", async () => {
			const server = createBff({ spaDir, passwordHash, publicOrigin: "https://hub.example.com", log: () => {} });
			expect((await login(server, { password })).status).toBe(403);
			expect((await login(server, { password }, { Origin: "https://hub.example.com" })).status).toBe(204);
		});
	});

	it("never logs the password, the hash, or the session id", async () => {
		const cookie = await sessionCookie(h.server);
		await login(h.server, { password: `${password}-wrong` });
		await request(h.server).get("/?token=secret-query").set("Cookie", cookie);
		const all = h.logs.join("\n");
		expect(all).not.toContain(password);
		expect(all).not.toContain(passwordHash);
		expect(all).not.toContain(cookie.split("=")[1]);
		expect(all).not.toContain("secret-query");
		expect(h.logs.some((l) => l.includes('"login failed"'))).toBe(true);
	});
});

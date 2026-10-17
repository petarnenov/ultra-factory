import { randomBytes } from "node:crypto";
import { connect } from "node:net";
import { pathToFileURL } from "node:url";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import type { Server } from "node:http";
import { tmpdir } from "node:os";
import path from "node:path";
import request from "supertest";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { hashPassword } from "../src/password.ts";
import { SESSION_COOKIE, SPA_CSP, createBff } from "../src/server.ts";

// A cheap scrypt cost keeps the suite fast; production uses DEFAULT_COST.
const TEST_COST = { ln: 10, r: 8, p: 1 };
const HOST = "hub.test";
const ORIGIN = `http://${HOST}`;

let spaDir: string;
let password: string;
let passwordHash: string;

beforeAll(async () => {
	spaDir = mkdtempSync(path.join(tmpdir(), "hub-bff-security-"));
	mkdirSync(path.join(spaDir, "assets"));
	writeFileSync(path.join(spaDir, "index.html"), "<!doctype html><title>spa</title>");
	writeFileSync(path.join(spaDir, "assets", "app-abc123.js"), "console.log('spa')");
	password = randomBytes(18).toString("base64url");
	passwordHash = await hashPassword(password, TEST_COST);
});

afterAll(() => rmSync(spaDir, { recursive: true, force: true }));

function bff(hash = passwordHash): Server {
	return createBff({ spaDir, passwordHash: hash, log: () => undefined });
}

function login(server: Server) {
	return request(server).post("/login").set("Host", HOST).set("Origin", ORIGIN).set("Sec-Fetch-Site", "same-origin").send({ password });
}

async function sessionCookie(server: Server): Promise<string> {
	const res = await login(server);
	expect(res.status).toBe(204);
	return (res.headers["set-cookie"] as unknown as string[])[0].split(";")[0];
}

const REQUIRED_HEADERS = {
	"strict-transport-security": "max-age=31536000; includeSubDomains",
	"x-content-type-options": "nosniff",
	"referrer-policy": "no-referrer",
	"x-frame-options": "DENY",
};

describe("BFF security headers", () => {
	let server: Server;
	beforeAll(() => {
		server = bff();
	});

	it.each([
		["the health check", () => request(server).get("/healthz")],
		["an unauthenticated API probe", () => request(server).get("/hub/hosts")],
		["the login page", () => request(server).get("/login")],
		[
			"the SPA shell",
			async () => {
				const cookie = await sessionCookie(server);
				return request(server).get("/").set("Cookie", cookie);
			},
		],
	])("sets the transport and content-type headers on %s", async (_name, send) => {
		const res = await send();
		for (const [name, value] of Object.entries(REQUIRED_HEADERS)) expect(res.headers[name]).toBe(value);
		expect(res.headers["permissions-policy"]).toContain("camera=()");
		expect(res.headers["permissions-policy"]).toContain("geolocation=()");
		expect(res.headers["permissions-policy"]).toContain("microphone=()");
	});

	it("sends the SPA content security policy on the shell and its assets", async () => {
		const cookie = await sessionCookie(server);
		for (const url of ["/", "/some/client/route", "/assets/app-abc123.js"]) {
			const res = await request(server).get(url).set("Cookie", cookie);
			expect(res.status).toBe(200);
			expect(res.headers["content-security-policy"]).toBe(SPA_CSP);
		}
	});

	it("keeps the SPA policy aligned with the web build's CSP meta tag", async () => {
		// The build injects this policy as a <meta> tag (RW-2.1.2). The header adds
		// only the directives a meta tag cannot carry. Both must stay identical so
		// the browser enforces one policy, not two that drift apart. The import is
		// a runtime path, so the hub-bff typecheck does not pull in the frontend.
		const cspModule = path.join(import.meta.dirname, "..", "..", "..", "frontend", "vite.renderer.csp.ts");
		const { contentSecurityPolicy } = (await import(pathToFileURL(cspModule).href)) as {
			contentSecurityPolicy: (mode: "build" | "serve", web: boolean) => string;
		};
		const meta = contentSecurityPolicy("build", true);
		expect(SPA_CSP).toBe(`${meta}; form-action 'self'; frame-ancestors 'none'`);
	});

	it("keeps the SPA policy free of loopback origins and wildcard schemes", () => {
		expect(SPA_CSP).not.toMatch(/127\.0\.0\.1|localhost/);
		expect(SPA_CSP).not.toMatch(/connect-src[^;]*\s(https?:|wss?:)(\s|;|$)/);
		expect(SPA_CSP).toContain("object-src 'none'");
		expect(SPA_CSP).toContain("frame-ancestors 'none'");
	});
});

describe("unauthenticated responses do not leak hosts", () => {
	let server: Server;
	beforeAll(() => {
		server = bff();
	});
	const SECRET_HOST = "h_secret123";
	const SECRET_URL = "http://worker-7.railway.internal:3011";

	it.each([
		"/hub/hosts",
		"/hub/hosts/h_secret123/probe",
		"/hub/workers",
		"/hub/sessions",
		`/hosts/${SECRET_HOST}/api/v1/sessions`,
		`/hosts/${SECRET_HOST}/api/v1/identity`,
		`/hosts/${SECRET_HOST}/mux`,
	])("answers %s with the bare 401 and no host id or URL", async (url) => {
		for (const method of ["get", "post", "delete"] as const) {
			const res = await request(server)[method](url).set("Host", HOST).set("Origin", ORIGIN).set("Accept", "application/json");
			expect(res.status).toBe(401);
			expect(res.body).toEqual({ error: "unauthorized" });
			expect(res.text).not.toContain(SECRET_HOST);
			expect(res.text).not.toContain("railway.internal");
		}
	});

	it("answers an unauthenticated WebSocket upgrade with 401 and no host detail", async () => {
		const res = await new Promise<string>((resolve) => {
			server.listen(0, "127.0.0.1", () => {
				const address = server.address();
				const port = typeof address === "object" && address ? address.port : 0;
				const socket = connect(port, "127.0.0.1", () => {
					socket.write(`GET /hosts/${SECRET_HOST}/mux HTTP/1.1\r\nHost: ${HOST}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\nOrigin: ${ORIGIN}\r\n\r\n`);
				});
				let data = "";
				socket.on("data", (chunk) => (data += chunk.toString()));
				socket.on("close", () => {
					server.close();
					resolve(data);
				});
			});
		});
		expect(res.startsWith("HTTP/1.1 401")).toBe(true);
		expect(res).not.toContain(SECRET_HOST);
		expect(res).not.toContain(SECRET_URL);
		expect(res).not.toContain("railway.internal");
	});
});

describe("session list and logout everywhere", () => {
	it("lists the live sessions and marks only the caller's own, without session ids", async () => {
		const server = bff();
		const mine = await sessionCookie(server);
		const other = await sessionCookie(server);
		const res = await request(server).get("/hub/sessions").set("Host", HOST).set("Cookie", mine);
		expect(res.status).toBe(200);
		expect(res.body.sessions).toHaveLength(2);
		expect(res.body.sessions.filter((s: { current: boolean }) => s.current)).toHaveLength(1);
		expect(res.text).not.toContain(mine.split("=")[1]);
		expect(res.text).not.toContain(other.split("=")[1]);
		expect(res.body.sessions[0]).toEqual({ createdAt: expect.any(Number), lastSeenAt: expect.any(Number), current: expect.any(Boolean) });
	});

	it("DELETE ends every session, including the caller's, and clears the cookie", async () => {
		const server = bff();
		const first = await sessionCookie(server);
		const second = await sessionCookie(server);
		const res = await request(server).delete("/hub/sessions").set("Host", HOST).set("Origin", ORIGIN).set("Cookie", second);
		expect(res.status).toBe(204);
		expect((res.headers["set-cookie"] as unknown as string[])[0]).toContain(`${SESSION_COOKIE}=;`);
		expect((res.headers["set-cookie"] as unknown as string[])[0]).toContain("Max-Age=0");
		expect((await request(server).get("/").set("Cookie", first)).status).toBe(401);
		expect((await request(server).get("/").set("Cookie", second)).status).toBe(401);
		expect((await request(server).get("/hub/sessions").set("Cookie", first)).status).toBe(401);
	});

	it("refuses DELETE from a foreign origin and leaves sessions intact", async () => {
		const server = bff();
		const cookie = await sessionCookie(server);
		const res = await request(server).delete("/hub/sessions").set("Host", HOST).set("Origin", "https://evil.test").set("Cookie", cookie);
		expect(res.status).toBe(403);
		expect((await request(server).get("/").set("Cookie", cookie)).status).toBe(200);
	});

	it("refuses DELETE without a session", async () => {
		const res = await request(bff()).delete("/hub/sessions").set("Host", HOST).set("Origin", ORIGIN);
		expect(res.status).toBe(401);
		expect(res.body).toEqual({ error: "unauthorized" });
	});

	it("rejects a session from before the operator password changed", async () => {
		// The password lives in an env hash; rotating it means a redeploy, which
		// builds a new server with an empty session store.
		const before = bff();
		const cookie = await sessionCookie(before);
		const rotatedHash = await hashPassword(`${password}-rotated`, TEST_COST);
		const after = bff(rotatedHash);
		expect((await request(after).get("/").set("Cookie", cookie)).status).toBe(401);
		expect((await request(before).get("/").set("Cookie", cookie)).status).toBe(200);
	});
});

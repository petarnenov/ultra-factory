import { createHash } from "node:crypto";
import { once } from "node:events";
import { createServer, request as httpRequest, type IncomingMessage, type Server } from "node:http";
import type { AddressInfo, Socket } from "node:net";
import request from "supertest";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { hashPassword } from "../src/password.ts";
import { createBff } from "../src/server.ts";
import type { ProxyHost } from "../src/proxy.ts";

const origin = "https://hub.test";
let servers: Server[];
let sockets: Set<Socket>;
let bff: Server;
let upstream: Server;
let cookie: string;
let host: ProxyHost;
let address: string;
let received: IncomingMessage[];
let hostLookups: string[];

async function listen(server: Server): Promise<string> {
	servers.push(server);
	server.on("connection", (socket) => { sockets.add(socket); socket.on("close", () => sockets.delete(socket)); });
	server.listen(0, "127.0.0.1");
	await once(server, "listening");
	return `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
}

beforeEach(async () => {
	servers = []; sockets = new Set(); received = []; hostLookups = [];
	upstream = createServer((req, res) => {
		received.push(req);
		if (req.url === "/api/reject") { res.writeHead(401).end("secret upstream body"); return; }
		res.setHeader("Set-Cookie", "ao_conn=upstream-secret");
		let body = "";
		req.on("data", (chunk) => { body += chunk; });
		req.on("end", () => { res.setHeader("Content-Type", "application/json"); res.end(JSON.stringify({ url: req.url, method: req.method, body })); });
	});
	host = { url: await listen(upstream), hostId: "h_test", bearer: "upstream-secret" };
	bff = createBff({ spaDir: "/unused", passwordHash: await hashPassword("test", { ln: 10, r: 8, p: 1 }), publicOrigin: origin, log: () => {}, hosts: { getHub: () => host, getHost: (id) => { hostLookups.push(id); return id === "h_test" ? host : undefined; } } });
	address = await listen(bff);
	const login = await request(bff).post("/login").set("Origin", origin).send({ password: "test" });
	cookie = login.headers["set-cookie"][0].split(";")[0];
});

afterEach(async () => {
	for (const socket of sockets) socket.destroy();
	await Promise.all(servers.map((server) => new Promise<void>((resolve) => server.close(() => resolve()))));
});

describe("REST proxy", () => {
	it.each([true, false])("rejects malformed path encoding without forwarding or disrupting a host (draining=%s)", async (draining) => {
		host.draining = draining;
		const response = await request(bff).post("/hosts/h_test/api/v1/sessions%E0").set({ Cookie: cookie, Origin: origin }).send({});
		// The existing safe-path gate rejects this before upstream resolution.
		expect(response.status).toBe(404);
		expect(received).toHaveLength(0);
		const healthy = await request(bff).get("/hosts/h_test/readyz").set("Cookie", cookie);
		expect(healthy.status).toBe(200);
		expect(received).toHaveLength(1);
	});
	it.each(["/api/items?q=a%2Fb", "/readyz", "/hosts/h_test/api/items?q=a%2Fb", "/hosts/h_test/readyz"])("routes %s and replaces untrusted headers", async (url) => {
		const res = await request(bff).get(url).set({ Cookie: `${cookie}; ao_conn=attacker`, Origin: origin, Authorization: "Bearer attacker", "CF-Connecting-IP": "203.0.113.1", "X-Forwarded-For": "203.0.113.2", "X-AO-Expected-Host-ID": "attacker", Accept: "application/json", "Last-Event-ID": "event-2" });
		expect(res.status).toBe(200);
		expect(res.body.url).toBe(url.replace("/hosts/h_test", ""));
		expect(res.headers["set-cookie"]).toBeUndefined();
		const headers = received[0].headers;
		expect(headers.authorization).toBe("Bearer upstream-secret");
		expect(headers["x-ao-expected-host-id"]).toBe("h_test");
		expect(headers.accept).toBe("application/json");
		expect(headers["last-event-id"]).toBe("event-2");
		for (const name of ["cookie", "origin", "cf-connecting-ip", "x-forwarded-for"]) expect(headers[name]).toBeUndefined();
	});
	it("streams request bodies and preserves content type", async () => {
		const res = await request(bff).post("/api/items").set({ Cookie: cookie, Origin: origin }).send({ item: 1 });
		expect(res.body).toMatchObject({ method: "POST", body: '{"item":1}' });
		expect(received[0].headers["content-type"]).toBe("application/json");
	});
	it.each(["/api/v1/sessions/s/preview/files/index.html", "/hosts/h_test/api/v1/sessions/s/preview/files/style.css?source=artifact"])("keeps preview credentials upstream only: %s", async (url) => {
		const res = await request(bff).get(url).set("Cookie", cookie);
		expect(res.status).toBe(200);
		expect(received[0].headers.cookie).toBe("ao_conn=upstream-secret");
		expect(res.headers["set-cookie"]).toBeUndefined();
	});
	it("maps credential rejection to 502 without exposing the upstream body", async () => {
		const res = await request(bff).get("/api/reject").set("Cookie", cookie);
		expect(res.status).toBe(502);
		expect(res.body).toEqual({ error: "host credential rejected", requestId: expect.any(String) });
	});
	it("maps an unreachable host to 502", async () => {
		await new Promise<void>((resolve) => upstream.close(() => resolve()));
		const res = await request(bff).get("/readyz").set("Cookie", cookie);
		expect(res.status).toBe(502);
		expect(res.body).toEqual({ error: "host unreachable", requestId: expect.any(String) });
	});
	it("refuses the primary daemon port", async () => {
		host.url = "http://127.0.0.1:3001";
		expect((await request(bff).get("/readyz").set("Cookie", cookie)).status).toBe(502);
		expect(received).toHaveLength(0);
	});
	it("keeps health local and rejects unknown hosts and non-proxy routes", async () => {
		expect((await request(bff).get("/healthz")).body).toEqual({ ok: true });
		for (const url of ["/hosts/missing/api/items", "/hosts/h_test/shutdown", "/mux-other"]) expect((await request(bff).get(url).set("Cookie", cookie)).status).toBe(404);
		expect(received).toHaveLength(0);
	});
	it("strips connection-nominated headers before injecting credentials", async () => {
		await request(bff).get("/api/items").set({ Cookie: cookie, Connection: "x-remove, authorization", "X-Remove": "secret" });
		expect(received[0].headers["x-remove"]).toBeUndefined();
		expect(received[0].headers.authorization).toBe("Bearer upstream-secret");
	});
});

it("streams three SSE events before upstream completion and aborts when the client disconnects", async () => {
	upstream.removeAllListeners("request");
	let closed!: () => void;
	const disconnected = new Promise<void>((resolve) => { closed = resolve; });
	upstream.on("request", (_req, res) => {
		res.writeHead(200, { "Content-Type": "text/event-stream" });
		let id = 0;
		const timer = setInterval(() => { if (id < 3) res.write(`id: ${++id}\ndata: event-${id}\n\n`); }, 15);
		res.on("close", () => { clearInterval(timer); closed(); });
	});
	await new Promise<void>((resolve, reject) => {
		const client = httpRequest(`${address}/api/events`, { headers: { Cookie: cookie } }, (res) => {
			expect(res.headers["content-type"]).toBe("text/event-stream");
			expect(res.headers["x-accel-buffering"]).toBe("no");
			let body = "";
			res.on("data", (chunk) => {
				body += chunk;
				if (body.includes("event-3")) {
					expect(body).toBe("id: 1\ndata: event-1\n\nid: 2\ndata: event-2\n\nid: 3\ndata: event-3\n\n");
					res.destroy(); resolve();
				}
			});
		});
		client.on("error", reject); client.end();
	});
	await disconnected;
});

function upgrade(url: string, headers: Record<string, string> = {}) {
	const req = httpRequest(address, { path: url, headers: { Connection: "Upgrade", Upgrade: "websocket", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ==", Origin: origin, Cookie: cookie, ...headers } });
	req.end();
	return req;
}

it.each(["/mux", "/hosts/h_test/mux"])("pipes WebSocket frames bidirectionally for %s", async (url) => {
	upstream.on("upgrade", (req, socket, head) => {
		received.push(req);
		const accept = createHash("sha1").update(`${req.headers["sec-websocket-key"]}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`).digest("base64");
		socket.write(`HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: ${accept}\r\nSet-Cookie: ao_conn=secret\r\n\r\n`);
		const echo = (data: Buffer) => {
			// The test sends one short, masked text frame containing "hi".
			const payload = Buffer.from(data.subarray(6));
			for (let i = 0; i < payload.length; i++) payload[i] ^= data[2 + i % 4];
			socket.write(Buffer.concat([Buffer.from([0x81, payload.length]), payload]));
		};
		if (head.length) echo(head);
		socket.on("data", echo);
	});
	const req = upgrade(url, { Authorization: "Bearer attacker", "CF-Connecting-IP": "203.0.113.1", "X-Forwarded-For": "203.0.113.2" });
	const [res, socket] = await once(req, "upgrade");
	expect(res.headers["set-cookie"]).toBeUndefined();
	socket.write(Buffer.from([0x81, 0x82, 1, 2, 3, 4, 104 ^ 1, 105 ^ 2]));
	const [data] = await once(socket, "data");
	expect(data).toEqual(Buffer.from([0x81, 2, 104, 105]));
	expect(received[0].headers.authorization).toBe("Bearer upstream-secret");
	expect(received[0].headers["x-ao-expected-host-id"]).toBe("h_test");
	for (const name of ["cookie", "origin", "cf-connecting-ip", "x-forwarded-for"]) expect(received[0].headers[name]).toBeUndefined();
	socket.destroy();
});

it.each([["Cookie", "", 401], ["Origin", "https://evil.test", 403], ["Origin", "", 403]] as const)("rejects WebSocket with %s=%s", async (key, value, status) => {
	const [res] = await once(upgrade("/mux", { [key]: value }), "response");
	res.resume();
	expect(res.statusCode).toBe(status);
	expect(received).toHaveLength(0);
});

it("maps upstream WebSocket credential rejection to 502", async () => {
	upstream.on("upgrade", (_req, socket) => socket.end("HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\n\r\n"));
	const [res] = await once(upgrade("/mux"), "response");
	let body = "";
	for await (const chunk of res) body += chunk;
	expect(res.statusCode).toBe(502);
	expect(JSON.parse(body)).toEqual({ error: "host credential rejected", requestId: expect.any(String) });
});

it("rejects invalid configured headers without crashing an upgrade", async () => {
	host.bearer = "invalid\r\nheader";
	const [res] = await once(upgrade("/mux"), "response");
	res.resume();
	expect(res.statusCode).toBe(502);
	expect(received).toHaveLength(0);
});

it("does not forward an unauthenticated identity probe", async () => {
	expect((await request(bff).get("/api/v1/identity")).status).toBe(401);
	expect(received).toHaveLength(0);
});

// Pass path separately: URL clients normalize dot segments before sending.
async function rawGet(path: string): Promise<number | undefined> {
	return new Promise((resolve, reject) => {
		const req = httpRequest(address, { path, headers: { Cookie: cookie } }, (res) => {
			res.resume();
			resolve(res.statusCode);
		});
		req.on("error", reject);
		req.end();
	});
}

const unsafePaths = [
	"/api/../shutdown", "/api/./items", "/api/%2e/items", "/api/%2E%2e/shutdown",
	"/api/.%2e/shutdown", "/api/%2e./shutdown", "/api//items",
	"/api/items%2fother", "/api/items%2Fother", "/api/items%5cother", "/api/items%5Cother",
	"/api/items\\other", "/api/%", "/api/%00", "//api/items",
	"/hosts/h_test/api/%2e%2e/%2e%2e/shutdown", "/hosts/h_test/api/../readyz",
	"/hosts/h_test//mux", "/hosts/h_test/./mux", "/hosts/h_test/%2e/mux",
	"/hosts/h_test/api/items%2fother", "/hosts/h_test/api/items%5cother",
];

it.each(unsafePaths)("rejects ambiguous HTTP path %s before normalization or lookup", async (path) => {
	expect(await rawGet(path)).toBe(404);
	expect(received).toHaveLength(0);
	expect(hostLookups).toHaveLength(0);
});

it.each(unsafePaths)("rejects ambiguous upgrade path %s before lookup", async (path) => {
	const [res] = await once(upgrade(path), "response");
	res.resume();
	expect(res.statusCode).toBe(404);
	expect(received).toHaveLength(0);
	expect(hostLookups).toHaveLength(0);
});

it.each(["h.test", "h:test", "h@test", "%68_test", "h%20test", "h%2ftest", "h%5ctest"])("rejects invalid host id %s without registry lookup", async (id) => {
	expect(await rawGet(`/hosts/${id}/api/items`)).toBe(404);
	const [res] = await once(upgrade(`/hosts/${id}/mux`), "response");
	res.resume();
	expect(res.statusCode).toBe(404);
	expect(hostLookups).toHaveLength(0);
	expect(received).toHaveLength(0);
});

it("preserves encoded query values and ordinary dot-containing path segments", async () => {
	const path = "/hosts/h_test/api/file.name?q=../%2f%5c//";
	expect(await rawGet(path)).toBe(200);
	expect(received[0].url).toBe(path.replace("/hosts/h_test", ""));
	expect(hostLookups).toEqual(["h_test"]);
});

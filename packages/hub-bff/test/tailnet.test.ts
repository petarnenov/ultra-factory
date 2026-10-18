import { createHash } from "node:crypto";
import { once } from "node:events";
import { createServer, request as httpRequest, type IncomingMessage, type Server } from "node:http";
import { connect, createServer as createTcpServer, type AddressInfo, type Server as TcpServer, type Socket } from "node:net";
import request from "supertest";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { probeHost, probeIdentity } from "../src/hosts-probe.ts";
import type { HostEntry } from "../src/hosts-store.ts";
import { hashPassword } from "../src/password.ts";
import type { ProxyHost } from "../src/proxy.ts";
import { createBff } from "../src/server.ts";
import { isTailnetDestination, parseTailnetSocks, setTailnetSocks, socksConnect, tailnetAgent } from "../src/tailnet.ts";
import { getSessionList } from "../src/workers-api.ts";

const origin = "https://hub.test";
const ENV_PROXIES = ["HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy", "NO_PROXY", "no_proxy"];

type SocksRequest = { atyp: number; host: string; port: number };
type FakeSocks = { port: number; requests: SocksRequest[]; reply: number; target: number; close: () => Promise<void> };

// fakeSocks is a minimal RFC 1928 server: no auth, CONNECT only. It records
// the requested destination and forwards every accepted CONNECT to target.
async function fakeSocks(target: number): Promise<FakeSocks> {
	const sockets = new Set<Socket>();
	const fake: FakeSocks = { port: 0, requests: [], reply: 0, target, close: async () => {} };
	const server: TcpServer = createTcpServer((client) => {
		sockets.add(client);
		client.on("close", () => sockets.delete(client));
		client.on("error", () => {});
		let buffer = Buffer.alloc(0);
		let greeted = false;
		const onData = (chunk: Buffer) => {
			buffer = Buffer.concat([buffer, chunk]);
			if (!greeted) {
				if (buffer.length < 3) return;
				greeted = true;
				buffer = buffer.subarray(2 + buffer[1]);
				client.write(Buffer.from([0x05, 0x00]));
			}
			if (buffer.length < 5) return;
			const atyp = buffer[3];
			const addressLength = atyp === 0x01 ? 4 : atyp === 0x04 ? 16 : 1 + buffer[4];
			if (buffer.length < 4 + addressLength + 2) return;
			const raw = buffer.subarray(4, 4 + addressLength);
			const host = atyp === 0x01 ? [...raw].join(".") : atyp === 0x04 ? (raw.toString("hex").match(/.{4}/g) ?? []).join(":") : raw.subarray(1).toString();
			const port = buffer.readUInt16BE(4 + addressLength);
			const rest = buffer.subarray(4 + addressLength + 2);
			client.off("data", onData);
			fake.requests.push({ atyp, host, port });
			if (fake.reply !== 0) { client.end(Buffer.from([0x05, fake.reply, 0x00, 0x01, 0, 0, 0, 0, 0, 0])); return; }
			const upstream = connect(fake.target, "127.0.0.1", () => {
				client.write(Buffer.from([0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0]));
				if (rest.length) upstream.write(rest);
				client.pipe(upstream).pipe(client);
			});
			sockets.add(upstream);
			upstream.on("close", () => { sockets.delete(upstream); client.destroy(); });
			upstream.on("error", () => client.destroy());
			client.on("close", () => upstream.destroy());
		};
		client.on("data", onData);
	});
	server.listen(0, "127.0.0.1");
	await once(server, "listening");
	fake.port = (server.address() as AddressInfo).port;
	fake.close = () => new Promise((resolve) => { for (const socket of sockets) socket.destroy(); server.close(() => resolve()); });
	return fake;
}

let closers: (() => Promise<void>)[];
let savedEnv: Record<string, string | undefined>;
let trapHits: number;

async function listen(server: Server | TcpServer): Promise<number> {
	const sockets = new Set<Socket>();
	server.on("connection", (socket: Socket) => { sockets.add(socket); socket.on("close", () => sockets.delete(socket)); });
	server.listen(0, "127.0.0.1");
	await once(server, "listening");
	closers.push(() => new Promise((resolve) => { for (const socket of sockets) socket.destroy(); server.close(() => resolve()); }));
	return (server.address() as AddressInfo).port;
}

beforeEach(async () => {
	closers = [];
	trapHits = 0;
	savedEnv = Object.fromEntries(ENV_PROXIES.map((name) => [name, process.env[name]]));
	// Any environment proxy points at a trap: the tailnet path must not read them.
	const trap = await listen(createTcpServer((socket) => { trapHits++; socket.destroy(); }));
	for (const name of ENV_PROXIES) process.env[name] = name.toLowerCase().startsWith("no") ? "" : `http://127.0.0.1:${trap}`;
});

afterEach(async () => {
	setTailnetSocks(undefined);
	for (const [name, value] of Object.entries(savedEnv)) { if (value === undefined) delete process.env[name]; else process.env[name] = value; }
	await Promise.all(closers.splice(0).map((close) => close()));
});

describe("AO_REMOTE_TAILNET_SOCKS validation", () => {
	it.each([["127.0.0.1:1055", "127.0.0.1", 1055], ["127.9.8.7:1", "127.9.8.7", 1], ["[::1]:65535", "::1", 65535]])("accepts %s", (value, host, port) => {
		expect(parseTailnetSocks(value)).toEqual({ host, port });
	});
	it("leaves the exception off when unset", () => {
		expect(parseTailnetSocks(undefined)).toBeUndefined();
		expect(parseTailnetSocks("")).toBeUndefined();
	});
	it.each(["localhost:1055", "0.0.0.0:1055", "10.0.0.1:1055", "100.64.0.1:1055", "[::2]:1055", "[::ffff:127.0.0.1]:1055", "::1:1055", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:65536", "127.1:1055", "[127.0.0.1]:1055", "socks5://127.0.0.1:1055", " 127.0.0.1:1055"])("rejects %s", (value) => {
		expect(() => parseTailnetSocks(value)).toThrow(/AO_REMOTE_TAILNET_SOCKS must be/);
	});
});

describe("tailnet routing predicate", () => {
	it.each(["100.64.0.0", "100.100.1.2", "100.127.255.255", "[fd7a:115c:a1e0::1]", "fd7a:115c:a1e0:ab12::1", "box.tail1234.ts.net", "BOX.Tail1234.TS.NET", "box.tail1234.ts.net."])("routes %s through SOCKS", (host) => {
		expect(isTailnetDestination(host)).toBe(true);
	});
	it.each(["100.63.255.255", "100.128.0.0", "127.0.0.1", "10.0.0.5", "[fd7a:115c:a1e1::1]", "[::1]", "[::ffff:100.64.0.1]", "ts.net", "box.ts.net.evil.com", "evilts.net", "box.ts.net..", "worker.up.railway.app", "100.100.1.2.nip.io"])("dials %s directly", (host) => {
		expect(isTailnetDestination(host)).toBe(false);
	});
	it("has no agent while the variable is unset", () => {
		expect(tailnetAgent(new URL("http://100.100.1.2:3011"))).toBeUndefined();
		setTailnetSocks({ host: "127.0.0.1", port: 1 });
		expect(tailnetAgent(new URL("http://100.100.1.2:3011"))).toBeDefined();
		expect(tailnetAgent(new URL("http://127.0.0.1:3011"))).toBeUndefined();
	});
});

// A fake host daemon: identity, an authenticated read, sessions, SSE, /mux.
async function fakeHost() {
	const seen: IncomingMessage[] = [];
	const server = createServer((req, res) => {
		seen.push(req);
		const json = (status: number, body: unknown) => res.writeHead(status, { "Content-Type": "application/json" }).end(JSON.stringify(body));
		if (req.url === "/api/v1/identity") return json(200, { hostId: "h_tail", apiVersion: 1 });
		if (req.headers.authorization !== "Bearer tail-secret") return json(401, { error: "unauthorized" });
		if (req.headers["x-ao-expected-host-id"] !== "h_tail") return json(421, { code: "HOST_ID_MISMATCH" });
		if (req.url === "/api/v1/projects") return json(200, { projects: [] });
		if (req.url === "/api/v1/sessions?active=true") return json(200, { sessions: [{ id: "s1", isTerminated: false, activity: { state: "active" } }] });
		if (req.url === "/api/redirect") { res.writeHead(302, { Location: "http://203.0.113.9/steal" }).end(); return; }
		if (req.url === "/api/events") {
			res.writeHead(200, { "Content-Type": "text/event-stream" });
			let id = 0;
			const timer = setInterval(() => { if (id < 3) res.write(`id: ${++id}\ndata: event-${id}\n\n`); }, 15);
			res.on("close", () => clearInterval(timer));
			return;
		}
		return json(200, { url: req.url });
	});
	server.on("upgrade", (req: IncomingMessage, socket: Socket, head: Buffer) => {
		seen.push(req);
		const accept = createHash("sha1").update(`${req.headers["sec-websocket-key"]}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`).digest("base64");
		socket.write(`HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`);
		const echo = (data: Buffer) => {
			const payload = Buffer.from(data.subarray(6));
			for (let i = 0; i < payload.length; i++) payload[i] ^= data[2 + i % 4];
			socket.write(Buffer.concat([Buffer.from([0x81, payload.length]), payload]));
		};
		if (head.length) echo(head);
		socket.on("data", echo);
	});
	const port = await listen(server);
	return { port, seen };
}

async function setup(hostUrl: (port: number) => string) {
	const host = await fakeHost();
	const socks = await fakeSocks(host.port);
	closers.push(socks.close);
	setTailnetSocks({ host: "127.0.0.1", port: socks.port });
	const proxyHost: ProxyHost = { url: hostUrl(host.port), hostId: "h_tail", bearer: "tail-secret" };
	const bff = createBff({ spaDir: "/unused", passwordHash: await hashPassword("test", { ln: 10, r: 8, p: 1 }), publicOrigin: origin, log: () => {}, hosts: { getHub: () => undefined, getHost: (id) => (id === "h_tail" ? proxyHost : undefined) } });
	const address = `http://127.0.0.1:${await listen(bff)}`;
	const login = await request(bff).post("/login").set("Origin", origin).send({ password: "test" });
	const cookie = login.headers["set-cookie"][0].split(";")[0] as string;
	return { host, socks, bff, address, cookie, url: proxyHost.url };
}

const entry = (url: string): HostEntry => ({ source: "manual", label: "tail", url, hostId: "h_tail", password: "tail-secret", status: "unknown" }) as HostEntry;

describe.each([
	["an IPv4 tailnet address", () => "http://100.100.1.2:3011", { atyp: 0x01, host: "100.100.1.2", port: 3011 }],
	["a MagicDNS name", () => "http://box.tail1234.ts.net:3011", { atyp: 0x03, host: "box.tail1234.ts.net", port: 3011 }],
	["an IPv6 tailnet address", () => "http://[fd7a:115c:a1e0::1]:3011", { atyp: 0x04, host: "fd7a:115c:a1e0:0000:0000:0000:0000:0001", port: 3011 }],
])("through SOCKS for %s", (_name, url, expected) => {
	it("probes identity and the bearer, and reads the workers session list", async () => {
		const h = await setup(url);
		expect(await probeHost(entry(h.url))).toEqual({ status: "online", hostId: "h_tail" });
		expect(await getSessionList(h.url, "tail-secret", "h_tail")).toHaveLength(1);
		expect(h.socks.requests).toEqual([expected, expected, expected]);
		expect(h.host.seen.map((req) => req.url)).toEqual(["/api/v1/identity", "/api/v1/projects", "/api/v1/sessions?active=true"]);
		expect(h.host.seen[1].headers).toMatchObject({ authorization: "Bearer tail-secret", "x-ao-expected-host-id": "h_tail" });
		expect(trapHits).toBe(0);
	});

	it("proxies a REST request with the host bearer and stripped client headers", async () => {
		const h = await setup(url);
		const res = await request(h.bff).get("/hosts/h_tail/api/items?q=1").set({ Cookie: h.cookie, Origin: origin, Authorization: "Bearer attacker", "X-Forwarded-For": "203.0.113.2" });
		expect(res.status).toBe(200);
		expect(res.body).toEqual({ url: "/api/items?q=1" });
		expect(h.socks.requests).toEqual([expected]);
		const headers = h.host.seen[0].headers;
		expect(headers.authorization).toBe("Bearer tail-secret");
		expect(headers["x-ao-expected-host-id"]).toBe("h_tail");
		for (const name of ["cookie", "origin", "x-forwarded-for"]) expect(headers[name]).toBeUndefined();
		expect(trapHits).toBe(0);
	});

	it("streams SSE events before the upstream completes", async () => {
		const h = await setup(url);
		await new Promise<void>((resolve, reject) => {
			const client = httpRequest(`${h.address}/hosts/h_tail/api/events`, { headers: { Cookie: h.cookie } }, (res) => {
				expect(res.headers["content-type"]).toBe("text/event-stream");
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
		expect(h.socks.requests).toEqual([expected]);
	});

	it("upgrades /mux and pipes WebSocket frames", async () => {
		const h = await setup(url);
		const req = httpRequest(h.address, { path: "/hosts/h_tail/mux", headers: { Connection: "Upgrade", Upgrade: "websocket", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ==", Origin: origin, Cookie: h.cookie } });
		req.end();
		const [, socket] = await once(req, "upgrade") as [IncomingMessage, Socket];
		socket.write(Buffer.from([0x81, 0x82, 1, 2, 3, 4, 104 ^ 1, 105 ^ 2]));
		const [data] = await once(socket, "data");
		expect(data).toEqual(Buffer.from([0x81, 2, 104, 105]));
		expect(h.socks.requests).toEqual([expected]);
		expect(h.host.seen[0].headers).toMatchObject({ authorization: "Bearer tail-secret", "x-ao-expected-host-id": "h_tail", upgrade: "websocket" });
		socket.destroy();
	});
});

describe("tailnet egress guards", () => {
	it("dials a non-tailnet host directly even with the variable set", async () => {
		const h = await setup((port) => `http://127.0.0.1:${port}`);
		expect(await probeHost(entry(h.url))).toEqual({ status: "online", hostId: "h_tail" });
		expect((await request(h.bff).get("/hosts/h_tail/api/items").set("Cookie", h.cookie)).status).toBe(200);
		expect(h.socks.requests).toEqual([]);
		expect(h.host.seen).toHaveLength(3);
	});

	it("never falls back to a direct dial when SOCKS refuses", async () => {
		const h = await setup(() => "http://100.100.1.2:3011");
		h.socks.reply = 0x02; // connection not allowed by ruleset
		expect(await probeIdentity(h.url)).toEqual({ ok: false, reason: "unreachable" });
		const res = await request(h.bff).get("/hosts/h_tail/api/items").set("Cookie", h.cookie);
		expect(res.status).toBe(502);
		expect(res.body.error).toBe("host unreachable");
		expect(h.socks.requests).toHaveLength(2);
		expect(h.host.seen).toHaveLength(0);
	});

	it("reports a tailnet host unreachable when the SOCKS server is down", async () => {
		const h = await setup(() => "http://100.100.1.2:3011");
		await h.socks.close();
		expect(await probeHost(entry(h.url))).toEqual({ status: "unreachable" });
		const req = httpRequest(h.address, { path: "/hosts/h_tail/mux", headers: { Connection: "Upgrade", Upgrade: "websocket", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ==", Origin: origin, Cookie: h.cookie } });
		req.end();
		const [res] = await once(req, "response") as [IncomingMessage];
		expect(res.statusCode).toBe(502);
		res.resume();
		expect(h.host.seen).toHaveLength(0);
		expect(trapHits).toBe(0);
	});

	it("returns a redirect without following it", async () => {
		const h = await setup(() => "http://100.100.1.2:3011");
		const res = await request(h.bff).get("/hosts/h_tail/api/redirect").set("Cookie", h.cookie);
		expect(res.status).toBe(302);
		expect(h.socks.requests).toHaveLength(1);
	});

	it("wraps https in TLS over the SOCKS socket with the tailnet name as SNI", async () => {
		let hello: Buffer | undefined;
		const tls = await listen(createTcpServer((socket) => socket.once("data", (chunk: Buffer) => { hello = chunk; socket.destroy(); })));
		const socks = await fakeSocks(tls);
		closers.push(socks.close);
		setTailnetSocks({ host: "127.0.0.1", port: socks.port });
		expect(await probeIdentity("https://box.tail1234.ts.net")).toEqual({ ok: false, reason: "unreachable" });
		expect(socks.requests).toEqual([{ atyp: 0x03, host: "box.tail1234.ts.net", port: 443 }]);
		expect(hello?.[0]).toBe(0x16); // TLS handshake record
		expect(hello?.includes(Buffer.from("box.tail1234.ts.net"))).toBe(true);
	});

	it("times out a SOCKS server that never answers", async () => {
		const silent = await listen(createTcpServer(() => {}));
		await expect(socksConnect({ host: "127.0.0.1", port: silent }, "100.100.1.2", 3011, 50)).rejects.toThrow(/timed out/);
	});
});

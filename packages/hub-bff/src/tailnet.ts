import { Agent as HttpAgent, request as httpRequest, type ClientRequestArgs } from "node:http";
import { Agent as HttpsAgent, request as httpsRequest } from "node:https";
import { BlockList, connect, isIP, type Socket } from "node:net";
import { Readable, type Duplex } from "node:stream";
import { connect as tlsConnect } from "node:tls";

// The tailnet egress exception from ADR 0008. When AO_REMOTE_TAILNET_SOCKS is
// set, tailnet destinations are dialed through the hub's loopback SOCKS5
// (tailscaled in userspace mode). Every other destination keeps the direct
// path it has today. Environment proxies are never read here, and a failed
// SOCKS dial never falls back to a direct one.

export type TailnetSocks = { host: string; port: number };

const SOCKS_DIAL_TIMEOUT_MS = 10_000;

const loopback = new BlockList();
loopback.addSubnet("127.0.0.0", 8, "ipv4");
loopback.addAddress("::1", "ipv6");

const tailnet = new BlockList();
tailnet.addSubnet("100.64.0.0", 10, "ipv4");
tailnet.addSubnet("fd7a:115c:a1e0::", 48, "ipv6");

// parseTailnetSocks validates AO_REMOTE_TAILNET_SOCKS ("127.0.0.1:1055" or
// "[::1]:1055"). Only a loopback IP literal is accepted, so name resolution
// cannot redirect the bearer to another proxy.
export function parseTailnetSocks(value: string | undefined): TailnetSocks | undefined {
	if (value === undefined || value === "") return undefined;
	const match = /^(?:\[([^\]]+)\]|([^:[\]]+)):(\d{1,5})$/.exec(value);
	const host = match?.[1] ?? match?.[2] ?? "";
	const family = isIP(host);
	const port = Number(match?.[3]);
	const bracketed = match?.[1] !== undefined;
	if (!match || !family || (family === 6) !== bracketed || !inList(loopback, host, family) || port < 1 || port > 65535) {
		throw new Error("AO_REMOTE_TAILNET_SOCKS must be host:port with a loopback IP literal (127.0.0.0/8 or [::1]), for example 127.0.0.1:1055");
	}
	return { host, port };
}

// isTailnetDestination decides routing from the name as written, never from
// a resolved address: an IP literal in 100.64.0.0/10 or fd7a:115c:a1e0::/48,
// or a hostname ending in .ts.net (one trailing dot allowed).
export function isTailnetDestination(hostname: string): boolean {
	let name = hostname.toLowerCase();
	if (name.startsWith("[") && name.endsWith("]")) name = name.slice(1, -1);
	const family = isIP(name);
	if (family) return inList(tailnet, name, family);
	if (name.endsWith(".")) name = name.slice(0, -1);
	return name.endsWith(".ts.net");
}

// BlockList matches IPv4 rules against IPv4-mapped IPv6 literals too; the ADR
// ranges are matched as written, so "::ffff:a.b.c.d" is in neither list.
function inList(list: BlockList, address: string, family: number): boolean {
	if (family === 6 && ipv6Bytes(address).subarray(0, 12).equals(Buffer.from([0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff]))) return false;
	return list.check(address, family === 6 ? "ipv6" : "ipv4");
}

let agents: { http: HttpAgent; https: HttpsAgent } | undefined;

// setTailnetSocks installs the validated proxy at startup. Unset keeps every
// destination on the direct path.
export function setTailnetSocks(value: TailnetSocks | undefined): void {
	agents?.http.destroy();
	agents?.https.destroy();
	agents = value ? { http: new TailnetHttpAgent(value), https: new TailnetHttpsAgent(value) } : undefined;
}

// tailnetAgent returns the SOCKS-backed agent for a tailnet destination while
// the exception is configured, and undefined for the unchanged direct path.
export function tailnetAgent(url: URL): HttpAgent | undefined {
	if (!agents || !isTailnetDestination(url.hostname)) return undefined;
	return url.protocol === "https:" ? agents.https : agents.http;
}

// fetchViaTailnet runs a fetch-shaped request over the tailnet agent, or
// returns undefined so the caller keeps its global fetch. Like fetch with
// redirect: "manual", a redirect is returned as-is and never followed.
export function fetchViaTailnet(url: string, init: { headers: Record<string, string>; signal: AbortSignal }): Promise<Response> | undefined {
	const target = new URL(url);
	const agent = tailnetAgent(target);
	if (!agent) return undefined;
	return new Promise((resolve, reject) => {
		const req = (target.protocol === "https:" ? httpsRequest : httpRequest)(target, { headers: init.headers, agent, signal: init.signal }, (reply) => {
			const status = reply.statusCode ?? 0;
			const headers = new Headers();
			for (const [key, value] of Object.entries(reply.headers)) {
				for (const item of Array.isArray(value) ? value : value === undefined ? [] : [value]) headers.append(key, item);
			}
			try {
				const nullBody = status === 204 || status === 205 || status === 304;
				if (nullBody) reply.resume();
				resolve(new Response(nullBody ? null : (Readable.toWeb(reply) as ReadableStream<Uint8Array>), { status, headers }));
			} catch (err) {
				reply.destroy();
				reject(err);
			}
		});
		req.on("error", reject);
		req.end();
	});
}

class TailnetHttpAgent extends HttpAgent {
	private readonly proxy: TailnetSocks;
	constructor(proxy: TailnetSocks) { super(); this.proxy = proxy; }
	override createConnection(options: ClientRequestArgs, callback?: (err: Error | null, stream: Duplex) => void): undefined {
		const { host, port } = destination(options, 80);
		socksConnect(this.proxy, host, port).then((socket) => callback?.(null, socket), (err: Error) => callback?.(err, undefined as never));
		return undefined;
	}
}

class TailnetHttpsAgent extends HttpsAgent {
	private readonly proxy: TailnetSocks;
	constructor(proxy: TailnetSocks) { super(); this.proxy = proxy; }
	override createConnection(options: ClientRequestArgs, callback?: (err: Error | null, stream: Duplex) => void): undefined {
		const { host, port } = destination(options, 443);
		socksConnect(this.proxy, host, port).then((socket) => {
			const servername = isIP(host) ? undefined : host.replace(/\.$/, "");
			callback?.(null, tlsConnect({ ...(options as object), socket, servername }));
		}, (err: Error) => callback?.(err, undefined as never));
		return undefined;
	}
}

function destination(options: ClientRequestArgs, defaultPort: number): { host: string; port: number } {
	const raw = options.hostname ?? options.host ?? "";
	const host = raw.startsWith("[") && raw.endsWith("]") ? raw.slice(1, -1) : raw;
	return { host, port: Number(options.port) || defaultPort };
}

// socksConnect opens a SOCKS5 CONNECT (RFC 1928, no authentication) to
// host:port. Hostnames are sent unresolved so the tailnet's MagicDNS answers.
export function socksConnect(proxy: TailnetSocks, host: string, port: number, timeoutMs = SOCKS_DIAL_TIMEOUT_MS): Promise<Socket> {
	return new Promise((resolve, reject) => {
		const request = connectRequest(host, port);
		const socket = connect({ host: proxy.host, port: proxy.port });
		let buffer = Buffer.alloc(0);
		let stage: "greeting" | "connect" = "greeting";
		const fail = (message: string) => { cleanup(); socket.destroy(); reject(new Error(`tailnet proxy: ${message}`)); };
		const timer = setTimeout(() => fail("timed out"), timeoutMs);
		const onError = () => fail("unreachable");
		const onClose = () => fail("closed the connection");
		const cleanup = () => { clearTimeout(timer); socket.off("data", onData); socket.off("error", onError); socket.off("close", onClose); };
		const onData = (chunk: Buffer) => {
			buffer = Buffer.concat([buffer, chunk]);
			if (stage === "greeting") {
				if (buffer.length < 2) return;
				if (buffer[0] !== 0x05 || buffer[1] !== 0x00) return fail("refused the greeting");
				buffer = buffer.subarray(2);
				stage = "connect";
				socket.write(request);
			}
			if (buffer.length < 5) return;
			if (buffer[0] !== 0x05 || buffer[1] !== 0x00) return fail(`refused the connection (reply ${buffer[1]})`);
			const length = buffer[3] === 0x01 ? 10 : buffer[3] === 0x04 ? 22 : buffer[3] === 0x03 ? 7 + buffer[4] : 0;
			if (!length) return fail("sent an invalid reply");
			if (buffer.length < length) return;
			cleanup();
			if (buffer.length > length) socket.unshift(buffer.subarray(length));
			resolve(socket);
		};
		socket.on("data", onData);
		socket.on("error", onError);
		socket.on("close", onClose);
		socket.once("connect", () => socket.write(Buffer.from([0x05, 0x01, 0x00])));
	});
}

function connectRequest(host: string, port: number): Buffer {
	const family = isIP(host);
	let address: Buffer;
	if (family === 4) {
		address = Buffer.from([0x01, ...host.split(".").map(Number)]);
	} else if (family === 6) {
		address = Buffer.concat([Buffer.from([0x04]), ipv6Bytes(host)]);
	} else {
		const name = Buffer.from(host, "utf8");
		if (!name.length || name.length > 255) throw new Error("tailnet proxy: hostname too long");
		address = Buffer.concat([Buffer.from([0x03, name.length]), name]);
	}
	const portBytes = Buffer.alloc(2);
	portBytes.writeUInt16BE(port);
	return Buffer.concat([Buffer.from([0x05, 0x01, 0x00]), address, portBytes]);
}

function ipv6Bytes(address: string): Buffer {
	const [head, tail = ""] = address.split("::");
	const parts = (text: string) => (text ? text.split(":") : []);
	// An embedded IPv4 tail ("::ffff:1.2.3.4") becomes its two final groups.
	const v4 = /^(.*:)(\d+\.\d+\.\d+\.\d+)$/.exec(address);
	if (v4) {
		const [a, b, c, d] = v4[2].split(".").map(Number);
		return ipv6Bytes(`${v4[1]}${((a << 8) | b).toString(16)}:${((c << 8) | d).toString(16)}`);
	}
	const left = parts(head);
	const right = parts(tail);
	const groups = address.includes("::") ? [...left, ...Array(8 - left.length - right.length).fill("0"), ...right] : left;
	const bytes = Buffer.alloc(16);
	groups.forEach((group, index) => bytes.writeUInt16BE(parseInt(group, 16), index * 2));
	return bytes;
}

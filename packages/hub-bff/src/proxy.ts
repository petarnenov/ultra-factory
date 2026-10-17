import { randomUUID } from "node:crypto";
import { request as httpRequest, validateHeaderValue, type IncomingHttpHeaders, type IncomingMessage, type ServerResponse } from "node:http";
import { request as httpsRequest } from "node:https";
import type { Duplex } from "node:stream";
import { tailnetAgent } from "./tailnet.ts";

export type ProxyHost = { url: string; hostId: string; bearer: string; draining?: boolean };
// RW-2.4.3 supplies paired registry entries, including the authenticated hub.
export type ProxyHosts = { getHost(id: string): ProxyHost | undefined; getHub(): ProxyHost | undefined };

// Validate before URL parsing can normalize traversal or separator spellings.
// Query values are opaque and may legitimately contain encoded paths.
export function hasSafeRequestPath(raw: string): boolean {
	const pathname = raw.split("?")[0];
	if (!pathname.startsWith("/") || pathname.includes("//")) return false;
	try {
		return pathname.split("/").every((segment) => {
			const decoded = decodeURIComponent(segment);
			return decoded !== "." && decoded !== ".." && !/[\\/\0]/.test(decoded);
		});
	} catch {
		return false;
	}
}

function route(raw: string): { id?: string; path: string } | undefined {
	if (!hasSafeRequestPath(raw)) return undefined;
	const match = /^(?:\/hosts\/([A-Za-z0-9_-]+))?(\/(?:api\/[^?]*|mux|readyz))(\?.*)?$/.exec(raw);
	if (!match) return undefined;
	return { id: match[1], path: match[2] + (match[3] ?? "") };
}

// Remove both standard hop-by-hop headers and fields nominated by Connection.
function clean(headers: IncomingHttpHeaders): IncomingHttpHeaders {
	const result = { ...headers };
	const nominated = (headers.connection ?? "").split(",").map((s) => s.trim().toLowerCase());
	for (const key of [...nominated, "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade", "set-cookie"]) delete result[key];
	return result;
}

// Only tailnet destinations get an agent; the rest keep the default one.
function agentFor(url: URL): { agent?: ReturnType<typeof tailnetAgent> } {
	const agent = tailnetAgent(url);
	return agent ? { agent } : {};
}

export function proxyError(res: ServerResponse, status: number, error: string): void {
	if (res.destroyed) return;
	if (res.headersSent) { res.destroy(); return; }
	const body = JSON.stringify({ error, requestId: randomUUID() });
	res.writeHead(status, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(body), "Cache-Control": "no-store" });
	res.end(body);
}

export function rejectUpgrade(socket: Duplex, status: number, error: string): void {
	const body = JSON.stringify({ error, requestId: randomUUID() });
	socket.end(`HTTP/1.1 ${status} Error\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
}

export function createProxy(hosts?: ProxyHosts) {
	function target(req: IncomingMessage) {
		const selected = route(req.url ?? "");
		if (!selected) return undefined;
		const host = selected.id ? hosts?.getHost(selected.id) : hosts?.getHub();
		if (!host) return undefined;
		const url = new URL(host.url);
		// B2 never allows the primary daemon port, even for a misconfigured entry.
		if (!["http:", "https:"].includes(url.protocol) || url.port === "3001" || url.username || url.password || url.pathname !== "/" || url.search || url.hash || !host.hostId || !host.bearer) throw new Error("invalid upstream");
		validateHeaderValue("Authorization", `Bearer ${host.bearer}`);
		validateHeaderValue("X-AO-Expected-Host-ID", host.hostId);
		const headers = clean(req.headers);
		for (const key of ["host", "authorization", "cookie", "origin", "cf-connecting-ip", "x-forwarded-for", "forwarded", "x-real-ip", "x-ao-expected-host-id"]) delete headers[key];
		headers.authorization = `Bearer ${host.bearer}`;
		headers["x-ao-expected-host-id"] = host.hostId;
		if (selected.path.split("?")[0].includes("/preview/files/")) headers.cookie = `ao_conn=${host.bearer}`;
		return { url, path: selected.path, headers, draining: host.draining };
	}

	return {
		http(req: IncomingMessage, res: ServerResponse): void {
			let dest;
			try { dest = target(req); } catch { proxyError(res, 502, "invalid host configuration"); return; }
			if (!dest) { proxyError(res, 404, "host or route not found"); return; }
			let pathname: string;
			try { pathname = decodeURIComponent(dest.path.split("?")[0]).replace(/\/$/, ""); }
			catch { proxyError(res, 400, "invalid request path"); return; }
			if (dest.draining && req.method === "POST" && (
				["/api/v1/sessions", "/api/v1/orchestrators", "/api/v1/orchestrators/delegate"].includes(pathname) ||
				/^\/api\/v1\/projects\/[^/]+\/tasks\/prepare$/.test(pathname) ||
				/^\/api\/v1\/sessions\/[^/]+\/(restore|resume-agent|reviews\/restore)$/.test(pathname)
			)) { proxyError(res, 409, "worker_draining"); return; }
			const upstream = (dest.url.protocol === "https:" ? httpsRequest : httpRequest)(dest.url, { method: req.method, path: dest.path, headers: dest.headers, ...agentFor(dest.url) }, (reply) => {
				if (reply.statusCode === 401) { reply.resume(); proxyError(res, 502, "host credential rejected"); return; }
				const headers = clean(reply.headers);
				if (String(headers["content-type"]).toLowerCase().startsWith("text/event-stream")) {
					headers["x-accel-buffering"] = "no";
					delete headers["content-length"];
				}
				res.writeHead(reply.statusCode ?? 502, headers);
				res.flushHeaders();
				reply.on("error", () => res.destroy());
				res.on("close", () => reply.destroy());
				reply.pipe(res);
			});
			upstream.on("error", () => proxyError(res, 502, "host unreachable"));
			req.on("aborted", () => upstream.destroy());
			req.on("error", () => upstream.destroy());
			res.on("close", () => upstream.destroy());
			req.socket.setTimeout(0);
			upstream.setTimeout(0);
			req.pipe(upstream);
		},
		upgrade(req: IncomingMessage, socket: Duplex, head: Buffer): void {
			let dest;
			try { dest = target(req); } catch { rejectUpgrade(socket, 502, "invalid host configuration"); return; }
			if (!dest || dest.path.split("?")[0] !== "/mux") { rejectUpgrade(socket, 404, "host or route not found"); return; }
			if (req.method !== "GET" || req.headers.upgrade?.toLowerCase() !== "websocket") { rejectUpgrade(socket, 400, "invalid upgrade"); return; }
			dest.headers.connection = "Upgrade";
			dest.headers.upgrade = "websocket";
			const upstream = (dest.url.protocol === "https:" ? httpsRequest : httpRequest)(dest.url, { path: dest.path, headers: dest.headers, ...agentFor(dest.url) });
			upstream.on("error", () => { if (!socket.destroyed) rejectUpgrade(socket, 502, "host unreachable"); });
			upstream.on("response", (reply) => {
				reply.resume();
				rejectUpgrade(socket, 502, reply.statusCode === 401 ? "host credential rejected" : "host upgrade rejected");
			});
			upstream.on("upgrade", (reply, remote, remoteHead) => {
				if (socket.destroyed) { remote.destroy(); return; }
				const headers = clean(reply.headers);
				headers.connection = "Upgrade";
				headers.upgrade = "websocket";
				socket.write(`HTTP/1.1 101 Switching Protocols\r\n${Object.entries(headers).flatMap(([key, value]) => (Array.isArray(value) ? value : [value]).map((v) => `${key}: ${v}\r\n`)).join("")}\r\n`);
				// 1011 is a server failure; never expose upstream error details.
				remote.on("error", () => socket.end(Buffer.from([0x88, 2, 0x03, 0xf3])));
				socket.on("error", () => remote.destroy());
				socket.on("close", () => remote.destroy());
				remote.on("close", () => socket.destroy());
				if (remoteHead.length) socket.write(remoteHead);
				if (head.length) remote.write(head);
				req.socket.setTimeout(0);
				remote.setTimeout(0);
				socket.pipe(remote).pipe(socket);
			});
			socket.on("error", () => upstream.destroy());
			socket.on("close", () => upstream.destroy());
			upstream.end();
		},
	};
}

import { createReadStream, readFileSync } from "node:fs";
import { stat } from "node:fs/promises";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import path from "node:path";
import type { Logger } from "./log.ts";
import { parsePasswordHash, verifyPassword, type ParsedHash } from "./password.ts";
import { clientKey, RateLimiter } from "./rate-limit.ts";
import { SESSION_ABSOLUTE_MS, SessionStore } from "./sessions.ts";

export const SESSION_COOKIE = "ao_hub_session";
export const LOGIN_ATTEMPTS_PER_MINUTE = 5;
const MAX_LOGIN_BODY_BYTES = 4096;
// Each scrypt check takes ~128 MiB. The login throttle is per client, so many
// distinct addresses at once could exhaust a small container's memory. Above
// this many concurrent checks, login answers 503 without calling scrypt.
export const MAX_CONCURRENT_PASSWORD_CHECKS = 2;

// Path prefixes owned by the daemon proxy of RW-2.4.2. Until it lands they
// answer 501 to an authenticated caller instead of falling through to the SPA.
const PROXY_PREFIXES = ["/api/", "/mux", "/hosts/", "/readyz"];

const LOGIN_DIR = path.join(import.meta.dirname, "..", "login");
const LOGIN_ASSETS: Record<string, { file: string; type: string }> = {
	"/login": { file: "login.html", type: "text/html; charset=utf-8" },
	"/_bff/login.css": { file: "login.css", type: "text/css; charset=utf-8" },
	"/_bff/login.js": { file: "login.js", type: "text/javascript; charset=utf-8" },
};
const LOGIN_CSP =
	"default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'";

const CONTENT_TYPES: Record<string, string> = {
	".html": "text/html; charset=utf-8",
	".js": "text/javascript; charset=utf-8",
	".mjs": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".map": "application/json; charset=utf-8",
	".svg": "image/svg+xml",
	".png": "image/png",
	".jpg": "image/jpeg",
	".jpeg": "image/jpeg",
	".gif": "image/gif",
	".webp": "image/webp",
	".ico": "image/x-icon",
	".woff": "font/woff",
	".woff2": "font/woff2",
	".ttf": "font/ttf",
	".wasm": "application/wasm",
	".txt": "text/plain; charset=utf-8",
};

export type BffOptions = {
	spaDir: string;
	passwordHash: string;
	// When set, state-changing requests must carry exactly this Origin.
	// Otherwise the Origin must match the request's Host.
	publicOrigin?: string;
	log: Logger;
	now?: () => number;
	// Tests only: replaces the password check.
	verify?: (candidate: string, hash: ParsedHash) => Promise<boolean>;
};

export function createBff(options: BffOptions): Server {
	const { log } = options;
	const spaDir = path.resolve(options.spaDir);
	const password = parsePasswordHash(options.passwordHash);
	const sessions = new SessionStore(options.now);
	const loginLimiter = new RateLimiter(LOGIN_ATTEMPTS_PER_MINUTE, 60_000, options.now);
	const verify = options.verify ?? verifyPassword;
	let passwordChecksInFlight = 0;
	const loginAssets = new Map(
		Object.entries(LOGIN_ASSETS).map(([route, a]) => [route, { body: readFileSync(path.join(LOGIN_DIR, a.file)), type: a.type }]),
	);

	async function handle(req: IncomingMessage, res: ServerResponse, pathname: string): Promise<void> {
		const method = req.method ?? "GET";
		const safe = method === "GET" || method === "HEAD";

		// Health check: fixed body, no auth, touches no daemon (ADR 0006).
		if (pathname === "/healthz" && safe) return sendJson(res, 200, { ok: true });

		// CSRF: SameSite=Strict on the cookie, plus a same-origin check on every
		// state-changing request, including login itself.
		if (!safe && !sameOrigin(req, options.publicOrigin)) {
			log("warn", "cross-origin request refused", { method, path: pathname });
			return sendJson(res, 403, { error: "cross_origin_request" });
		}

		if (pathname === "/login" && method === "POST") return login(req, res);
		if (pathname === "/logout" && method === "POST") {
			sessions.destroy(readCookie(req, SESSION_COOKIE));
			res.setHeader("Set-Cookie", sessionCookie("", 0));
			res.writeHead(204).end();
			return;
		}

		const loginAsset = safe ? loginAssets.get(pathname) : undefined;
		if (loginAsset) {
			if (pathname === "/login" && sessions.touch(readCookie(req, SESSION_COOKIE))) return redirect(res, "/");
			res.writeHead(200, {
				"Content-Type": loginAsset.type,
				"Content-Length": loginAsset.body.length,
				"Cache-Control": "no-cache",
				"Content-Security-Policy": LOGIN_CSP,
			});
			res.end(method === "HEAD" ? undefined : loginAsset.body);
			return;
		}

		// Everything below requires a session, including the SPA shell.
		if (!sessions.touch(readCookie(req, SESSION_COOKIE))) {
			if (safe && isNavigation(req)) return redirect(res, "/login");
			return sendJson(res, 401, { error: "unauthorized" });
		}

		if (PROXY_PREFIXES.some((p) => pathname === p.replace(/\/$/, "") || pathname.startsWith(p))) {
			return sendJson(res, 501, { error: "not_implemented" });
		}
		if (!safe) return sendJson(res, 405, { error: "method_not_allowed" });
		return serveSpa(req, res, pathname);
	}

	async function login(req: IncomingMessage, res: ServerResponse): Promise<void> {
		const client = clientKey(req);
		const retryAfter = loginLimiter.hit(client);
		if (retryAfter > 0) {
			log("warn", "login rate limited", { client });
			res.setHeader("Retry-After", String(retryAfter));
			return sendJson(res, 429, { error: "too_many_attempts" });
		}
		if (!/^application\/json\b/i.test(req.headers["content-type"] ?? "")) {
			return sendJson(res, 415, { error: "expected_json" });
		}
		let candidate: unknown;
		try {
			candidate = (JSON.parse(await readBody(req, MAX_LOGIN_BODY_BYTES)) as { password?: unknown })?.password;
		} catch {
			return sendJson(res, 400, { error: "invalid_request" });
		}
		if (typeof candidate !== "string" || candidate.length === 0) {
			log("warn", "login failed", { client });
			return sendJson(res, 401, { error: "invalid_credentials" });
		}
		if (passwordChecksInFlight >= MAX_CONCURRENT_PASSWORD_CHECKS) {
			log("warn", "login refused: password checks busy", { client });
			res.setHeader("Retry-After", "1");
			return sendJson(res, 503, { error: "busy" });
		}
		passwordChecksInFlight += 1;
		let ok: boolean;
		try {
			ok = await verify(candidate, password);
		} finally {
			passwordChecksInFlight -= 1;
		}
		if (!ok) {
			log("warn", "login failed", { client });
			return sendJson(res, 401, { error: "invalid_credentials" });
		}
		const id = sessions.create();
		log("info", "login succeeded", { client });
		res.setHeader("Set-Cookie", sessionCookie(id, Math.floor(SESSION_ABSOLUTE_MS / 1000)));
		res.writeHead(204).end();
	}

	async function serveSpa(req: IncomingMessage, res: ServerResponse, pathname: string): Promise<void> {
		let file = path.join(spaDir, pathname);
		if (file !== spaDir && !file.startsWith(spaDir + path.sep)) return sendJson(res, 404, { error: "not_found" });
		let info = await stat(file).catch(() => undefined);
		if (info?.isDirectory()) {
			file = path.join(file, "index.html");
			info = await stat(file).catch(() => undefined);
		}
		if (!info?.isFile()) {
			// Client-side routes have no extension; missing assets do.
			if (path.extname(pathname)) return sendJson(res, 404, { error: "not_found" });
			file = path.join(spaDir, "index.html");
			info = await stat(file);
		}
		const isIndex = file === path.join(spaDir, "index.html");
		res.writeHead(200, {
			"Content-Type": CONTENT_TYPES[path.extname(file).toLowerCase()] ?? "application/octet-stream",
			"Content-Length": info.size,
			// Vite emits content-hashed names under assets/; the shell must revalidate.
			"Cache-Control": !isIndex && pathname.startsWith("/assets/") ? "public, max-age=31536000, immutable" : "no-cache",
		});
		if (req.method === "HEAD") {
			res.end();
			return;
		}
		createReadStream(file)
			.on("error", () => res.destroy())
			.pipe(res);
	}

	return createServer((req, res) => {
		const started = Date.now();
		let pathname: string;
		try {
			pathname = decodeURIComponent(new URL(req.url ?? "/", "http://bff.invalid").pathname);
		} catch {
			pathname = "";
		}
		res.setHeader("X-Content-Type-Options", "nosniff");
		res.setHeader("Referrer-Policy", "no-referrer");
		res.setHeader("X-Frame-Options", "DENY");
		res.on("finish", () => {
			// Log the decoded path only: never the query string, headers, or body.
			log("info", "request", { method: req.method, path: pathname, status: res.statusCode, ms: Date.now() - started });
		});
		if (!pathname || pathname.includes("\0")) {
			sendJson(res, 400, { error: "invalid_path" });
			return;
		}
		handle(req, res, pathname).catch((err: unknown) => {
			log("error", "request failed", { path: pathname, error: err instanceof Error ? err.message : String(err) });
			if (!res.headersSent) sendJson(res, 500, { error: "internal_error" });
			else res.destroy();
		});
	});
}

// sameOrigin accepts a state-changing request only when the browser says it is
// same-origin. A missing Origin is refused: every browser sends it on POST.
function sameOrigin(req: IncomingMessage, publicOrigin: string | undefined): boolean {
	const site = req.headers["sec-fetch-site"];
	if (site !== undefined && site !== "same-origin") return false;
	const origin = req.headers.origin;
	if (!origin) return false;
	if (publicOrigin) return origin === publicOrigin;
	try {
		const url = new URL(origin);
		return (url.protocol === "https:" || url.protocol === "http:") && url.host === req.headers.host;
	} catch {
		return false;
	}
}

function isNavigation(req: IncomingMessage): boolean {
	return req.headers["sec-fetch-mode"] === "navigate" || (req.headers.accept ?? "").includes("text/html");
}

function sessionCookie(value: string, maxAgeSeconds: number): string {
	return `${SESSION_COOKIE}=${value}; HttpOnly; Secure; SameSite=Strict; Path=/; Max-Age=${maxAgeSeconds}`;
}

function readCookie(req: IncomingMessage, name: string): string | undefined {
	for (const part of (req.headers.cookie ?? "").split(";")) {
		const eq = part.indexOf("=");
		if (eq > 0 && part.slice(0, eq).trim() === name) return part.slice(eq + 1).trim() || undefined;
	}
	return undefined;
}

function readBody(req: IncomingMessage, limit: number): Promise<string> {
	return new Promise((resolve, reject) => {
		const chunks: Buffer[] = [];
		let size = 0;
		req.on("data", (chunk: Buffer) => {
			size += chunk.length;
			if (size > limit) {
				// Drain the rest so a 400 can still be written.
				req.removeAllListeners("data");
				req.resume();
				reject(new Error("body too large"));
				return;
			}
			chunks.push(chunk);
		});
		req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
		req.on("error", reject);
	});
}

function sendJson(res: ServerResponse, status: number, body: unknown): void {
	const payload = JSON.stringify(body);
	res.writeHead(status, { "Content-Type": "application/json; charset=utf-8", "Content-Length": Buffer.byteLength(payload), "Cache-Control": "no-store" });
	res.end(payload);
}

function redirect(res: ServerResponse, location: string): void {
	res.writeHead(302, { Location: location, "Cache-Control": "no-store" }).end();
}

import type { IncomingMessage, ServerResponse } from "node:http";
import type { Logger } from "./log.ts";
import type { AuditLog } from "./audit.ts";
import { checkAuth, probeIdentity, type HostsMonitor } from "./hosts-probe.ts";
import { HostsValidationError, normalizeHostUrl, type HostsStore } from "./hosts-store.ts";
import { readBody, sendJson } from "./server.ts";

const MAX_HOST_BODY_BYTES = 4096;

export type HostsApiOptions = {
	store: HostsStore;
	monitor: HostsMonitor;
	log: Logger;
	audit?: AuditLog;
	// Tests only: lets fake hosts on 127.0.0.1 be added.
	allowLoopback?: boolean;
};

export type HostsApi = (req: IncomingMessage, res: ServerResponse, pathname: string) => Promise<void>;

const HOSTS_API_PREFIX = "/hub/hosts";

// createHostsApi serves the authenticated host registry routes. The caller has
// already checked the session and, for state-changing methods, the Origin.
// Responses never include a host's password or token.
//
//	GET    /hub/hosts            list hosts with their last status
//	POST   /hub/hosts            {label,url,password}: probe identity, check the bearer, save
//	DELETE /hub/hosts/:id        remove the record (nothing in Railway is deleted)
//	POST   /hub/hosts/:id/probe  probe now and return the host
export function createHostsApi(options: HostsApiOptions): HostsApi {
	const { store, monitor, log, audit } = options;

	async function add(req: IncomingMessage, res: ServerResponse): Promise<void> {
		if (!/^application\/json\b/i.test(req.headers["content-type"] ?? "")) return sendJson(res, 415, { error: "expected_json" });
		let body: { label?: unknown; url?: unknown; password?: unknown };
		try {
			body = JSON.parse(await readBody(req, MAX_HOST_BODY_BYTES)) as typeof body;
			if (!body || typeof body !== "object") throw new Error("not an object");
		} catch {
			return sendJson(res, 400, { error: "invalid_request" });
		}
		let url: string;
		try {
			url = normalizeHostUrl(body.url, options.allowLoopback);
			if (store.labelTaken(body.label)) throw new HostsValidationError("label_taken", "a host with this label already exists");
			if (typeof body.password !== "string" || !body.password) throw new HostsValidationError("invalid_password", "password is required");
		} catch (err) {
			if (err instanceof HostsValidationError) return sendJson(res, err.code === "label_taken" ? 409 : 400, { error: err.code, message: err.message });
			throw err;
		}

		const identity = await probeIdentity(url);
		if (!identity.ok) {
			log("warn", "host add refused", { url, reason: identity.reason });
			return sendJson(res, 502, {
				error: identity.reason === "unsupported_api_version" ? "unsupported_api_version" : "host_unreachable",
				message: identity.reason === "unsupported_api_version" ? "the host speaks an unsupported AO API version" : "the host did not answer GET /api/v1/identity",
			});
		}
		const { hostId } = identity.identity;
		const status = await checkAuth(url, hostId, body.password);
		if (status !== "online") {
			log("warn", "host add refused", { url, hostId, reason: status });
			// Not 401: the browser would read that as its own session ending.
			if (status === "auth_failed") return sendJson(res, 422, { error: "host_credential_rejected", message: "the host rejected the password" });
			return sendJson(res, 502, { error: "host_unreachable", message: "the host did not answer an authenticated request" });
		}

		try {
			await audit?.record("host.add", hostId);
			const host = await store.add({ label: body.label, url, password: body.password, hostId, status });
			log("info", "host added", { label: host.label, hostId, url });
			return sendJson(res, 201, { host });
		} catch (err) {
			if (err instanceof HostsValidationError) return sendJson(res, err.code === "label_taken" || err.code === "host_exists" ? 409 : 400, { error: err.code, message: err.message });
			throw err;
		}
	}

	return async (req, res, pathname) => {
		const method = req.method ?? "GET";
		const rest = pathname.slice(HOSTS_API_PREFIX.length).replace(/\/$/, "");
		if (rest === "") {
			if (method === "GET" || method === "HEAD") return sendJson(res, 200, { hosts: store.list() });
			if (method === "POST") return add(req, res);
			return sendJson(res, 405, { error: "method_not_allowed" });
		}
		const match = /^\/([^/]+)(\/probe)?$/.exec(rest);
		if (!match) return sendJson(res, 404, { error: "not_found" });
		const id = match[1];
		if (match[2]) {
			if (method !== "POST") return sendJson(res, 405, { error: "method_not_allowed" });
			const host = await monitor.probe(id);
			if (!host) return sendJson(res, 404, { error: "not_found" });
			return sendJson(res, 200, { host });
		}
		if (method !== "DELETE") return sendJson(res, 405, { error: "method_not_allowed" });
		try {
			if (!store.get(id)) return sendJson(res, 404, { error: "not_found" });
			await audit?.record("host.remove", id);
			if (!(await store.remove(id))) return sendJson(res, 404, { error: "not_found" });
		} catch (err) {
			if (err instanceof HostsValidationError) return sendJson(res, 409, { error: err.code, message: err.message });
			throw err;
		}
		log("info", "host removed", { hostId: id });
		res.writeHead(204).end();
	};
}

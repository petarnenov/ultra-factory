import { randomBytes, randomUUID } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import type { Logger } from "./log.ts";
import { probeIdentity } from "./hosts-probe.ts";
import { HostsValidationError, type HostsStore } from "./hosts-store.ts";
import type { WorkerProvider } from "./railway-client.ts";
import { readBody, sendJson } from "./server.ts";

const PREFIX = "/hub/workers";
const MAX_BODY_BYTES = 16 * 1024;
const POLL_INTERVAL_MS = 5_000;
const PROVISION_TIMEOUT_MS = 10 * 60_000;
const SECRET_KEYS = ["ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "GH_TOKEN"] as const;

export type WorkersApiOptions = {
	store: HostsStore;
	provider?: WorkerProvider;
	image?: string;
	urlMode?: "public" | "private";
	log: Logger;
	now?: () => number;
	sleep?: (milliseconds: number) => Promise<void>;
	probe?: (url: string) => Promise<{ ok: true; identity: { hostId: string } } | { ok: false }>;
	activeSessions?: (url: string, password: string, hostId: string) => Promise<unknown[]>;
};

export type WorkersApi = (req: IncomingMessage, res: ServerResponse, pathname: string) => Promise<void>;

export function createWorkersApi(options: WorkersApiOptions): WorkersApi {
	const now = options.now ?? Date.now;
	const sleep = options.sleep ?? ((ms) => new Promise((resolve) => setTimeout(resolve, ms)));
	const probe = options.probe ?? probeIdentity;

	async function create(req: IncomingMessage, res: ServerResponse): Promise<void> {
		if (!options.provider || !options.image) return sendJson(res, 503, { error: "worker_provisioning_unavailable" });
		if (!/^application\/json\b/i.test(req.headers["content-type"] ?? "")) return sendJson(res, 415, { error: "expected_json" });
		let body: { label?: unknown; size?: unknown; env?: Record<string, unknown> };
		try {
			body = JSON.parse(await readBody(req, MAX_BODY_BYTES)) as typeof body;
			if (!body || typeof body !== "object" || Array.isArray(body)) throw new Error();
		} catch {
			return sendJson(res, 400, { error: "invalid_request" });
		}
		if (body.size !== undefined) return sendJson(res, 400, { error: "unsupported_size", message: "Railway volume sizing is not verified; omit size" });
		const env = body.env;
		if (!env || typeof env !== "object" || Array.isArray(env)) return sendJson(res, 400, { error: "invalid_environment" });
		const variables: Record<string, string> = {};
		for (const key of SECRET_KEYS) {
			const value = env[key];
			if (value !== undefined) {
				if (typeof value !== "string" || !value) return sendJson(res, 400, { error: "invalid_environment" });
				variables[key] = value;
			}
		}
		for (const key of ["GH_TOKEN", "AO_GIT_USER_NAME", "AO_GIT_USER_EMAIL"] as const) {
			if (typeof env[key] !== "string" || !env[key]) return sendJson(res, 400, { error: "invalid_environment" });
			variables[key] = env[key] as string;
		}
		const id = randomUUID();
		const password = randomBytes(24).toString("base64url");
		try {
			await options.store.createWorker({ id, label: body.label, password });
		} catch (err) {
			if (err instanceof HostsValidationError) return sendJson(res, err.code === "label_taken" ? 409 : 400, { error: err.code, message: err.message });
			throw err;
		}
		variables.AO_REMOTE_PASSWORD = password;
		const name = `ao-worker-${id.slice(0, 8)}`;
		void provision(id, { name, image: options.image, env: variables, volumeMountPath: "/data", urlMode: options.urlMode ?? "public" }).catch(() => undefined);
		return sendJson(res, 202, { worker: options.store.getPublic(id) });
	}

	async function provision(id: string, spec: Parameters<NonNullable<WorkersApiOptions["provider"]>["create"]>[0]): Promise<void> {
		try {
			const ids = await options.provider!.create(spec);
			await options.store.updateWorker(id, { railway: { serviceId: ids.serviceId, volumeId: ids.volumeId, environmentId: ids.environmentId }, url: ids.url });
			const deadline = now() + PROVISION_TIMEOUT_MS;
			while (now() <= deadline) {
				const status = await options.provider!.getStatus(ids.serviceId);
				if (status === "failed" || status === "stopped") {
					await options.store.updateWorker(id, { status: "failed", lastError: "Railway deployment failed" });
					return;
				}
				if (status === "running") {
					const identity = await probe(ids.url);
					if (identity.ok) {
						await options.store.updateWorker(id, { hostId: identity.identity.hostId, status: "online" });
						return;
					}
				}
				if (now() >= deadline) break;
				await sleep(Math.min(POLL_INTERVAL_MS, deadline - now()));
			}
			await options.store.updateWorker(id, { status: "failed", lastError: "Worker did not answer its identity probe within 10 minutes" });
		} catch (error) {
			const message = error instanceof Error ? error.message : "Railway operation failed";
			await options.store.updateWorker(id, { status: "failed", lastError: safeProviderError(message) });
			options.log("error", "worker provisioning failed", { id, error: safeProviderError(message) });
		}
	}

	async function remove(res: ServerResponse, id: string, force: boolean): Promise<void> {
		const host = options.store.get(id);
		if (!host || host.source !== "railway") return sendJson(res, 404, { error: "not_found" });
		if (!force && host.hostId) {
			let active: unknown[];
			try {
				if (!host.hostId) return sendJson(res, 409, { error: "session_check_failed" });
				const sessions = await (options.activeSessions ?? getSessionList)(host.url, host.password, host.hostId);
				active = nonTerminatedSessions(sessions);
			} catch {
				return sendJson(res, 409, { error: "session_check_failed" });
			}
			if (active.length) return sendJson(res, 409, { error: "active_sessions", sessions: active });
		}
		if (!host.railway) return sendJson(res, 409, { error: "worker_still_provisioning" });
		await options.store.updateWorker(id, { status: "removing", desired: "removed", lastError: undefined });
		try {
			if (!options.provider) throw new Error("Worker provider is unavailable");
			await options.provider.delete(host.railway.serviceId, host.railway.volumeId);
		} catch {
			await options.store.updateWorker(id, { status: "failed", desired: "removed", lastError: "Railway could not delete this worker; the registry record was retained for cleanup" });
			return sendJson(res, 502, { error: "worker_delete_failed", worker: options.store.getPublic(id) });
		}
		await options.store.removeWorker(id);
		res.writeHead(204).end();
	}

	return async (req, res, pathname) => {
		const method = req.method ?? "GET";
		const rest = pathname.slice(PREFIX.length).replace(/\/$/, "");
		if (!rest) {
			if (method === "POST") return create(req, res);
			return sendJson(res, 405, { error: "method_not_allowed" });
		}
		const match = /^\/([^/]+)$/.exec(rest);
		if (!match) return sendJson(res, 404, { error: "not_found" });
		if (method !== "DELETE") return sendJson(res, 405, { error: "method_not_allowed" });
		const force = new URL(req.url ?? "/", "http://bff.invalid").searchParams.get("force") === "1";
		return remove(res, match[1], force);
	};
}

function safeProviderError(message: string): string {
	if (/auth|permission|authoriz/i.test(message)) return "Railway authorization failed";
	if (/quota|limit/i.test(message)) return "Railway resource quota was exceeded";
	return "Railway operation failed";
}

async function getSessionList(url: string, password: string, hostId: string): Promise<unknown[]> {
	const response = await fetch(`${url}/api/v1/sessions?active=true`, {
		headers: { Authorization: `Bearer ${password}`, "X-AO-Expected-Host-ID": hostId, Accept: "application/json" },
		redirect: "manual",
		signal: AbortSignal.timeout(5_000),
	});
	if (!response.ok) throw new Error("Unable to inspect worker sessions");
	const body = await response.json() as { sessions?: unknown };
	if (!Array.isArray(body.sessions)) throw new Error("Worker returned an invalid session list");
	return body.sessions;
}

function nonTerminatedSessions(raw: unknown[]): unknown[] {
	const sessions = raw as Array<{ id?: unknown; displayName?: unknown; isTerminated?: unknown; activity?: { state?: unknown } }>;
	for (const session of sessions) {
		if (!session || typeof session !== "object" || typeof session.id !== "string" || typeof session.isTerminated !== "boolean") {
			throw new Error("Worker returned an invalid session list");
		}
		if (session.isTerminated === false && typeof session.activity?.state !== "string") throw new Error("Worker returned an invalid session list");
	}
	return sessions.filter((session) => session.isTerminated === false).map((session) => ({
		id: session.id,
		...(typeof session.displayName === "string" ? { displayName: session.displayName } : {}),
		activity: { state: session.activity!.state as string },
	}));
}

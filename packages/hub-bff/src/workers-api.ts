import { randomBytes, randomUUID } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import type { Logger } from "./log.ts";
import type { AuditLog } from "./audit.ts";
import { probeIdentity } from "./hosts-probe.ts";
import { HostsValidationError, type HostsStore } from "./hosts-store.ts";
import type { RailwayService, WorkerProvider, WorkerSpec } from "./railway-client.ts";
import { readBody, sendJson } from "./server.ts";

const PREFIX = "/hub/workers";
const MAX_BODY_BYTES = 16 * 1024;
const POLL_INTERVAL_MS = 5_000;
const PROVISION_TIMEOUT_MS = 10 * 60_000;
const RECONCILE_INTERVAL_MS = 5 * 60_000;
const WORKER_SERVICE_PREFIX = "ao-worker-";
const SECRET_KEYS = ["ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "GH_TOKEN"] as const;

export type WorkersApiOptions = {
	store: HostsStore;
	provider?: WorkerProvider;
	image?: string;
	urlMode?: "public" | "private";
	log: Logger;
	audit?: AuditLog;
	now?: () => number;
	sleep?: (milliseconds: number) => Promise<void>;
	probe?: (url: string) => Promise<{ ok: true; identity: { hostId: string } } | { ok: false }>;
	activeSessions?: (url: string, password: string, hostId: string) => Promise<unknown[]>;
	maxWorkers?: number;
};

export type WorkersApi = ((req: IncomingMessage, res: ServerResponse, pathname: string) => Promise<void>) & { start(): void; stop(): void };

export function createWorkersApi(options: WorkersApiOptions): WorkersApi {
	const now = options.now ?? Date.now;
	const sleep = options.sleep ?? ((ms) => new Promise((resolve) => setTimeout(resolve, ms)));
	const probe = options.probe ?? probeIdentity;
	const maxWorkers = options.maxWorkers ?? 5;
	let reconcileTimer: ReturnType<typeof setInterval> | undefined;
	let reconciling: Promise<void> | undefined;
	let lastReconcile: string | undefined;
	let orphans: RailwayService[] = [];

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
			await options.store.createWorker({ id, label: body.label, password, maxWorkers });
		} catch (err) {
			if (err instanceof HostsValidationError) {
				if (err.code === "worker_limit_reached") {
					const count = options.store.listRailwayWorkers().length;
					return sendJson(res, 429, { error: err.code, count, limit: maxWorkers, message: `Worker limit reached (${count}/${maxWorkers})` });
				}
				return sendJson(res, err.code === "label_taken" ? 409 : 400, { error: err.code, message: err.message });
			}
			throw err;
		}
		try {
			await options.audit?.record("worker.create", id);
		} catch (error) {
			await options.store.updateWorker(id, { status: "failed", lastError: "Worker creation audit could not be written" });
			throw error;
		}
		variables.AO_REMOTE_PASSWORD = password;
		const name = `ao-worker-${id.slice(0, 8)}`;
		void provision(id, { name, image: options.image, env: variables, volumeMountPath: "/data", urlMode: options.urlMode ?? "public" }).catch(() => undefined);
		return sendJson(res, 202, { worker: options.store.getPublic(id) });
	}

	async function pollProvisioning(id: string, serviceId: string, url: string): Promise<void> {
		try {
			const deadline = now() + PROVISION_TIMEOUT_MS;
			while (now() <= deadline) {
				const status = await options.provider!.getStatus(serviceId);
				if (status === "failed" || status === "stopped") {
					await options.store.updateWorker(id, { status: "failed", lastError: "Railway deployment failed" });
					return;
				}
				if (status === "running") {
					const identity = await probe(url);
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

	async function provision(id: string, spec: WorkerSpec): Promise<void> {
		try {
			const ids = await options.provider!.create(spec);
			await options.store.updateWorker(id, { railway: { serviceId: ids.serviceId, volumeId: ids.volumeId, environmentId: ids.environmentId }, url: ids.url });
			await pollProvisioning(id, ids.serviceId, ids.url);
		} catch (error) {
			const message = error instanceof Error ? error.message : "Railway operation failed";
			await options.store.updateWorker(id, { status: "failed", lastError: safeProviderError(message) });
			options.log("error", "worker provisioning failed", { id, error: safeProviderError(message) });
		}
	}

	async function deleteRecordedWorker(id: string, serviceId: string, volumeId: string): Promise<void> {
		try {
			if (!options.provider) throw new Error("Worker provider is unavailable");
			await options.provider.delete(serviceId, volumeId);
			await options.store.removeWorker(id);
		} catch (error) {
			const message = error instanceof Error ? error.message : "Railway operation failed";
			await options.store.updateWorker(id, { status: "failed", desired: "removed", lastError: "Railway could not delete this worker; the registry record was retained for cleanup" });
			options.log("warn", "worker removal recovery failed", { id, error: safeProviderError(message) });
		}
	}

	function workerCounts(): Record<string, number> {
		const counts: Record<string, number> = {};
		for (const worker of options.store.listRailwayWorkers()) counts[worker.status] = (counts[worker.status] ?? 0) + 1;
		return counts;
	}

	async function reconcile(): Promise<void> {
		if (reconciling) return reconciling;
		reconciling = (async () => {
			try {
				if (!options.provider) throw new Error("Railway provider is unavailable");
				const services = (await options.provider.list()).filter((service) => service.name.startsWith(WORKER_SERVICE_PREFIX));
				const registered = new Set(options.store.listRailwayWorkers().flatMap((worker) => worker.railway?.serviceId ? [worker.railway.serviceId] : []));
				orphans = services.filter((service) => !registered.has(service.id));
				for (const orphan of orphans) options.log("warn", "orphan Railway worker detected", { serviceId: orphan.id, serviceName: orphan.name });
				lastReconcile = new Date(now()).toISOString();
				options.log("info", "worker reconciliation complete", { workersByStatus: JSON.stringify(workerCounts()), lastReconcile, orphanCount: orphans.length });
			} catch (error) {
				options.log("error", "worker reconciliation failed", { error: safeProviderError(error instanceof Error ? error.message : "Railway operation failed"), workersByStatus: JSON.stringify(workerCounts()), lastReconcile });
			}
		})();
		try { await reconciling; } finally { reconciling = undefined; }
	}

	async function resumeJobs(): Promise<void> {
		for (const worker of options.store.listRailwayWorkers()) {
			if (worker.status === "provisioning") {
				if (worker.railway?.serviceId && worker.url) {
					void pollProvisioning(worker.id ?? worker.hostId ?? "", worker.railway.serviceId, worker.url);
				} else {
					const id = worker.id ?? "";
					await options.store.updateWorker(id, { status: "failed", lastError: "Provisioning was interrupted before Railway resource IDs were recorded" });
				}
			} else if (worker.status === "removing" && worker.railway) {
				void deleteRecordedWorker(worker.id ?? worker.hostId ?? "", worker.railway.serviceId, worker.railway.volumeId);
			}
		}
	}

	function start(): void {
		if (reconcileTimer) return;
		void resumeJobs().then(() => reconcile());
		reconcileTimer = setInterval(() => { void reconcile(); }, RECONCILE_INTERVAL_MS);
		reconcileTimer.unref?.();
	}

	function stop(): void {
		if (reconcileTimer) clearInterval(reconcileTimer);
		reconcileTimer = undefined;
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
		if (force) await options.audit?.record("worker.force_kill", id);
		await options.audit?.record("worker.delete", id);
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

	const api = async (req: IncomingMessage, res: ServerResponse, pathname: string): Promise<void> => {
		const method = req.method ?? "GET";
		const rest = pathname.slice(PREFIX.length).replace(/\/$/, "");
		if (!rest) {
			if (method === "GET") return sendJson(res, 200, { workers: options.store.list(), orphans, lastReconcile, workersByStatus: workerCounts(), limit: maxWorkers });
			if (method === "POST") return create(req, res);
			return sendJson(res, 405, { error: "method_not_allowed" });
		}
		const match = /^\/([^/]+)$/.exec(rest);
		if (!match) return sendJson(res, 404, { error: "not_found" });
		if (method !== "DELETE") return sendJson(res, 405, { error: "method_not_allowed" });
		const force = new URL(req.url ?? "/", "http://bff.invalid").searchParams.get("force") === "1";
		return remove(res, match[1], force);
	};
	Object.assign(api, { start, stop });
	return api as WorkersApi;
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

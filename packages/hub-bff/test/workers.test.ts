import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import request from "supertest";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { HostsStore } from "../src/hosts-store.ts";
import type { WorkerProvider, WorkerSpec } from "../src/railway-client.ts";
import { createBff } from "../src/server.ts";
import { createWorkersApi } from "../src/workers-api.ts";
import { hashPassword } from "../src/password.ts";
import type { AuditEntry, AuditEvent, AuditLog } from "../src/audit.ts";

const operatorPassword = "operator-password";
const origin = "http://hub.test";
let passwordHash: string;
let dir: string;
let servers: ReturnType<typeof createBff>[] = [];

beforeAll(async () => { passwordHash = await hashPassword(operatorPassword, { ln: 10, r: 8, p: 1 }); });
afterEach(async () => {
	for (const server of servers) await new Promise<void>((resolve) => server.close(() => resolve()));
	servers = [];
	if (dir) rmSync(dir, { recursive: true, force: true });
});

function fakeProvider(overrides: Partial<WorkerProvider> = {}): WorkerProvider & { specs: WorkerSpec[] } {
	const specs: WorkerSpec[] = [];
	return {
		specs,
		create: vi.fn(async (spec: WorkerSpec) => { specs.push(spec); return { serviceId: "service-1", volumeId: "volume-1", environmentId: "environment-1", url: "https://worker.up.railway.app" }; }),
		getStatus: vi.fn(async () => "running" as const),
		list: vi.fn(async () => []),
		setVariables: vi.fn(async () => {}),
		redeploy: vi.fn(async () => {}),
		delete: vi.fn(async () => {}),
		...overrides,
	};
}

async function setup(provider: WorkerProvider, extras: Partial<Parameters<typeof createWorkersApi>[0]> = {}) {
	dir = mkdtempSync(path.join(tmpdir(), "hub-workers-"));
	const spa = path.join(dir, "spa");
	mkdirSync(spa);
	writeFileSync(path.join(spa, "index.html"), "ok");
	const store = await HostsStore.open(path.join(dir, "data"));
	const auditEntries: AuditEntry[] = [];
	const audit: AuditLog = {
		async record(event: AuditEvent, subjectId?: string) { auditEntries.push({ ts: new Date().toISOString(), event, ...(subjectId ? { subjectId } : {}) }); },
		async list(limit: number) { return auditEntries.slice(-limit); },
	};
	const workersApi = createWorkersApi({ store, provider, image: "example/worker:latest", log: () => {}, audit, probe: async () => ({ ok: true, identity: { hostId: "worker-host" } }), ...extras });
	const server = createBff({ spaDir: spa, passwordHash, log: () => {}, workersApi, audit, hosts: store, verify: async (candidate) => candidate === operatorPassword });
	servers.push(server);
	const login = await request(server).post("/login").set("Host", "hub.test").set("Origin", origin).send({ password: operatorPassword });
	const cookie = (login.headers["set-cookie"] as unknown as string[])[0].split(";")[0];
	return { server, store, cookie, workersApi, auditEntries };
}

function createWorker(server: ReturnType<typeof createBff>, cookie: string, body = { label: "worker-a", env: { GH_TOKEN: "gh-secret", AO_GIT_USER_NAME: "Agent", AO_GIT_USER_EMAIL: "agent@example.com" } }) {
	return request(server).post("/hub/workers").set("Host", "hub.test").set("Origin", origin).set("Cookie", cookie).send(body);
}

async function provisioned(server: ReturnType<typeof createBff>, cookie: string, store: HostsStore) {
	const response = await createWorker(server, cookie);
	const id = response.body.worker.id as string;
	await vi.waitFor(() => expect(store.getPublic(id)?.status).toBe("online"));
	return id;
}

function deleteWorker(server: ReturnType<typeof createBff>, cookie: string, id: string, force = false) {
	return request(server).delete(`/hub/workers/${id}${force ? "?force=1" : ""}`).set("Host", "hub.test").set("Origin", origin).set("Cookie", cookie);
}

describe("Railway worker lifecycle", () => {
	it("persists draining through probes and restart, blocks creation, and allows explicit release", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { activeSessions: async () => [] });
		const id = await provisioned(h.server, h.cookie, h.store);
		const drain = await request(h.server).post(`/hub/workers/${id}/drain`).set("Host", "hub.test").set("Origin", origin).set("Cookie", h.cookie);
		expect(drain.status).toBe(200);
		expect(drain.body).toMatchObject({ readyForRedeploy: true, worker: { status: "draining" } });
		await h.store.recordProbe(id, { status: "online" });
		const reopened = await HostsStore.open(path.join(dir, "data"));
		expect(reopened.getPublic(id)?.status).toBe("draining");
		for (const route of ["sessions", "orchestrators", "orchestrators/delegate", "projects/example/tasks/prepare", "sessions/example/restore", "sessions/example/resume-agent", "sessions/example/reviews/restore", "%73essions"]) {
			const blocked = await request(h.server).post(`/hosts/${id}/api/v1/${route}`).set("Host", "hub.test").set("Origin", origin).set("Cookie", h.cookie).send({});
			expect(blocked.status).toBe(409);
			expect(blocked.body.error).toBe("worker_draining");
		}
		expect(provider.redeploy).not.toHaveBeenCalled();
		expect(provider.delete).not.toHaveBeenCalled();
		const release = await request(h.server).delete(`/hub/workers/${id}/drain`).set("Host", "hub.test").set("Origin", origin).set("Cookie", h.cookie);
		expect(release.status).toBe(200);
		expect(h.store.getHost(id)?.draining).toBeUndefined();
	});

	it.each(["idle", "waiting_input"])("does not call a worker empty while a %s session remains", async (state) => {
		const h = await setup(fakeProvider(), { activeSessions: async () => [{ id: "s1", isTerminated: false, activity: { state }, secret: "hidden" }] });
		const id = await provisioned(h.server, h.cookie, h.store);
		const result = await request(h.server).post(`/hub/workers/${id}/drain`).set("Host", "hub.test").set("Origin", origin).set("Cookie", h.cookie);
		expect(result.body.readyForRedeploy).toBe(false);
		expect(result.body.sessions).toHaveLength(1);
		expect(result.text).not.toContain("hidden");
	});

	it("keeps draining when the session check fails and requires login and origin", async () => {
		const h = await setup(fakeProvider(), { activeSessions: async () => { throw new Error("unreachable"); } });
		const id = await provisioned(h.server, h.cookie, h.store);
		const route = `/hub/workers/${id}/drain`;
		expect((await request(h.server).post(route).set("Host", "hub.test").set("Origin", origin)).status).toBe(401);
		expect((await request(h.server).post(route).set("Cookie", h.cookie)).status).toBe(403);
		const result = await request(h.server).post(route).set("Host", "hub.test").set("Origin", origin).set("Cookie", h.cookie);
		expect(result.status).toBe(409);
		expect(result.body).toMatchObject({ readyForRedeploy: false, worker: { status: "draining" } });
	});

	it("reports worker management unavailable without a provider and quietly skips reconcile", async () => {
		const logs: Array<{ level: string; message: string }> = [];
		const h = await setup(undefined as never, { log: (level, message) => logs.push({ level, message }) });
		h.workersApi.start();
		await vi.waitFor(async () => {
			const result = await request(h.server).get("/hub/workers").set("Cookie", h.cookie);
			expect(result.body).toMatchObject({ orphans: [], providerConfigured: false });
		});
		expect(logs).toContainEqual({ level: "info", message: "Railway worker management not configured" });
		expect(logs.some(({ level }) => level === "error")).toBe(false);
		h.workersApi.stop();
	});

	it("writes a provisioning record before create and reaches online with a 32-character password", async () => {
		const provider = fakeProvider();
		const h = await setup(provider);
		const response = await createWorker(h.server, h.cookie);
		expect(response.status).toBe(202);
		const id = response.body.worker.id as string;
		await vi.waitFor(() => expect(h.store.getPublic(id)?.status).toBe("online"));
		const saved = h.store.get(id)!;
		expect(saved).toMatchObject({ hostId: "worker-host", status: "online", desired: "running", railway: { serviceId: "service-1", volumeId: "volume-1", environmentId: "environment-1" } });
		expect(h.store.list()[0]?.id).toBe(id);
		await h.store.recordProbe(id, { status: "unreachable" });
		expect(h.store.getPublic(id)).toMatchObject({ id, status: "unreachable" });
		expect(provider.specs[0]?.env.AO_REMOTE_PASSWORD).toHaveLength(32);
		expect(response.text).not.toContain("gh-secret");
		expect(saved.password).not.toBe("gh-secret");
		expect(h.auditEntries).toContainEqual(expect.objectContaining({ event: "worker.create", subjectId: id }));
	});

	it("uses the injected worker password only when the fake-mode seam is supplied", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { workerPassword: () => "fixed-e2e-worker-password" });
		const response = await createWorker(h.server, h.cookie);
		const id = response.body.worker.id as string;
		await vi.waitFor(() => expect(h.store.getPublic(id)?.status).toBe("online"));
		expect(provider.specs[0]?.env.AO_REMOTE_PASSWORD).toBe("fixed-e2e-worker-password");
	});

	it("retains a failed record after Railway create errors without leaking error text", async () => {
		const provider = fakeProvider({ create: vi.fn(async () => { throw new Error("Not Authorized secret-value"); }) });
		const h = await setup(provider);
		const response = await createWorker(h.server, h.cookie);
		const id = response.body.worker.id as string;
		await vi.waitFor(() => expect(h.store.getPublic(id)?.status).toBe("failed"));
		expect(h.store.get(id)?.lastError).toBe("Railway authorization failed");
		expect(JSON.stringify(h.store.list())).not.toContain("secret-value");
	});

	it("marks a running deployment failed when the identity probe times out", async () => {
		let now = 0;
		const provider = fakeProvider();
		const h = await setup(provider, { now: () => now, sleep: async (ms) => { now += ms; }, probe: async () => ({ ok: false }) });
		const response = await createWorker(h.server, h.cookie);
		const id = response.body.worker.id as string;
		await vi.waitFor(() => expect(h.store.getPublic(id)?.status).toBe("failed"));
		expect(h.store.get(id)?.lastError).toContain("10 minutes");
		expect(h.store.get(id)?.railway?.serviceId).toBe("service-1");
	});

	it("returns orphan services from the periodic registry comparison and never deletes them", async () => {
		const provider = fakeProvider({ list: vi.fn(async () => [
			{ id: "orphan-service", name: "ao-worker-orphan" },
			{ id: "unrelated-service", name: "api" },
		]) });
		const h = await setup(provider);
		h.workersApi.start();
		await vi.waitFor(async () => {
			const result = await request(h.server).get("/hub/workers").set("Cookie", h.cookie);
			expect(result.body.orphans).toEqual([{ id: "orphan-service", name: "ao-worker-orphan" }]);
		});
		expect(provider.delete).not.toHaveBeenCalled();
		h.workersApi.stop();
	});

	it("resumes a persisted provisioning record after reopening the registry", async () => {
		const provider = fakeProvider();
		const h = await setup(provider);
		await h.store.createWorker({ id: "persisted-worker", label: "persisted", password: "worker-password" });
		await h.store.updateWorker("persisted-worker", {
			url: "https://worker.up.railway.app",
			railway: { serviceId: "service-1", volumeId: "volume-1", environmentId: "environment-1" },
		});
		const reopened = await HostsStore.open(path.join(dir, "data"));
		const resumedApi = createWorkersApi({ store: reopened, provider, image: "example/worker:latest", log: () => {}, probe: async () => ({ ok: true, identity: { hostId: "resumed-host" } }) });
		resumedApi.start();
		await vi.waitFor(() => expect(reopened.getPublic("persisted-worker")?.status).toBe("online"));
		expect(provider.getStatus).toHaveBeenCalledWith("service-1");
		resumedApi.stop();
	});

	it("resumes a persisted removal once and retains failed deletes for manual cleanup", async () => {
		const provider = fakeProvider({ delete: vi.fn(async () => { throw new Error("denied"); }) });
		const h = await setup(provider);
		await h.store.createWorker({ id: "removing-worker", label: "removing", password: "worker-password" });
		await h.store.updateWorker("removing-worker", {
			url: "https://worker.up.railway.app",
			railway: { serviceId: "service-1", volumeId: "volume-1", environmentId: "environment-1" },
			status: "removing",
			desired: "removed",
		});
		const reopened = await HostsStore.open(path.join(dir, "data"));
		const resumedApi = createWorkersApi({ store: reopened, provider, log: () => {} });
		resumedApi.start();
		await vi.waitFor(() => expect(reopened.getPublic("removing-worker")).toMatchObject({ status: "failed", desired: "removed" }));
		await new Promise((resolve) => setTimeout(resolve, 10));
		expect(provider.delete).toHaveBeenCalledTimes(1);
		expect(reopened.getPublic("removing-worker")).toBeDefined();
		resumedApi.stop();
	});

	it("enforces the worker limit atomically and returns the count and limit", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { maxWorkers: 1 });
		expect((await createWorker(h.server, h.cookie)).status).toBe(202);
		const limited = await createWorker(h.server, h.cookie, { label: "worker-b", env: { GH_TOKEN: "gh-secret", AO_GIT_USER_NAME: "Agent", AO_GIT_USER_EMAIL: "agent@example.com" } });
		expect(limited.status).toBe(429);
		expect(limited.body).toMatchObject({ error: "worker_limit_reached", count: 1, limit: 1 });
		expect(provider.create).toHaveBeenCalledTimes(1);
	});

	it("validates a requested volume size against AO_HUB_MAX_VOLUME_GB before reporting sizing unsupported", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { maxVolumeGb: 10 });
		const env = { GH_TOKEN: "gh-secret", AO_GIT_USER_NAME: "Agent", AO_GIT_USER_EMAIL: "agent@example.com" };
		const tooLarge = await createWorker(h.server, h.cookie, { label: "worker-a", size: 50, env } as never);
		expect(tooLarge.status).toBe(400);
		expect(tooLarge.body).toMatchObject({ error: "volume_size_limit", size: 50, limit: 10 });
		const invalid = await createWorker(h.server, h.cookie, { label: "worker-a", size: "big", env } as never);
		expect(invalid.body).toMatchObject({ error: "invalid_size" });
		const withinLimit = await createWorker(h.server, h.cookie, { label: "worker-a", size: 5, env } as never);
		expect(withinLimit.body).toMatchObject({ error: "unsupported_size" });
		expect(provider.create).not.toHaveBeenCalled();
		expect(h.store.listRailwayWorkers()).toHaveLength(0);
	});

	it("flags an online worker idle after AO_HUB_IDLE_STOP_HOURS without sessions and never stops it", async () => {
		let now = Date.parse("2026-10-17T00:00:00.000Z");
		let sessions: unknown[] = [];
		let failing = false;
		const provider = fakeProvider();
		const h = await setup(provider, { now: () => now, idleStopHours: 2, activeSessions: async () => { if (failing) throw new Error("down"); return sessions; } });
		const id = await provisioned(h.server, h.cookie, h.store);
		const idle = async () => (await request(h.server).get("/hub/workers").set("Cookie", h.cookie)).body;

		await h.workersApi.checkIdle();
		now += 60 * 60_000;
		expect(await idle()).toMatchObject({ idle: [], idleStopHours: 2 });
		now += 60 * 60_000;
		expect((await idle()).idle).toEqual([{ id, idleSince: "2026-10-17T00:00:00.000Z" }]);

		sessions = [{ id: "s", isTerminated: false, activity: { state: "active" } }];
		await h.workersApi.checkIdle();
		expect((await idle()).idle).toEqual([]);

		sessions = [];
		await h.workersApi.checkIdle();
		now += 3 * 60 * 60_000;
		failing = true;
		await h.workersApi.checkIdle();
		expect((await idle()).idle).toEqual([]);
		expect(provider.delete).not.toHaveBeenCalled();
		expect(h.store.getPublic(id)?.status).toBe("online");
	});

	it("reports no idle workers when AO_HUB_IDLE_STOP_HOURS is unset", async () => {
		const activeSessions = vi.fn(async () => []);
		const h = await setup(fakeProvider(), { activeSessions });
		await provisioned(h.server, h.cookie, h.store);
		await h.workersApi.checkIdle();
		const body = (await request(h.server).get("/hub/workers").set("Cookie", h.cookie)).body;
		expect(body.idle).toEqual([]);
		expect(body.idleStopHours).toBeUndefined();
		expect(activeSessions).not.toHaveBeenCalled();
	});

	it("deletes when the session list is empty", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { activeSessions: async () => [] });
		const id = await provisioned(h.server, h.cookie, h.store);
		expect((await deleteWorker(h.server, h.cookie, id)).status).toBe(204);
		expect(h.store.getPublic(id)).toBeUndefined();
		expect(provider.delete).toHaveBeenCalledWith("service-1", "volume-1");
		expect(h.auditEntries).toContainEqual(expect.objectContaining({ event: "worker.delete", subjectId: id }));
	});

	it("deletes when all listed sessions are terminated", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { activeSessions: async () => [{ id: "done", isTerminated: true, status: "terminated" }] });
		const id = await provisioned(h.server, h.cookie, h.store);
		expect((await deleteWorker(h.server, h.cookie, id)).status).toBe(204);
		expect(h.store.getPublic(id)).toBeUndefined();
	});

	it.each(["idle", "waiting_input"])("blocks deletion for a non-terminated %s session and returns only safe fields", async (state) => {
		const provider = fakeProvider();
		const h = await setup(provider, { activeSessions: async () => [{ id: "session-1", displayName: "Worker task", isTerminated: false, activity: { state }, secret: "must-not-return" }] });
		const id = await provisioned(h.server, h.cookie, h.store);
		const blocked = await deleteWorker(h.server, h.cookie, id);
		expect(blocked.status).toBe(409);
		expect(blocked.body).toEqual({ error: "active_sessions", sessions: [{ id: "session-1", displayName: "Worker task", activity: { state } }] });
		expect(provider.delete).not.toHaveBeenCalled();
	});

	it("fails closed when the worker session list cannot be fetched", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { activeSessions: async () => { throw new Error("unreachable"); } });
		const id = await provisioned(h.server, h.cookie, h.store);
		const blocked = await deleteWorker(h.server, h.cookie, id);
		expect(blocked.status).toBe(409);
		expect(blocked.body).toEqual({ error: "session_check_failed" });
		expect(provider.delete).not.toHaveBeenCalled();
	});

	it("allows force deletion when the session list cannot be fetched", async () => {
		const provider = fakeProvider();
		const h = await setup(provider, { activeSessions: async () => { throw new Error("unreachable"); } });
		const id = await provisioned(h.server, h.cookie, h.store);
		expect((await deleteWorker(h.server, h.cookie, id, true)).status).toBe(204);
		expect(provider.delete).toHaveBeenCalledWith("service-1", "volume-1");
		expect(h.auditEntries).toContainEqual(expect.objectContaining({ event: "worker.force_kill", subjectId: id }));
	});

	it("retains a failed record when Railway denies deletion", async () => {
		const provider = fakeProvider({ delete: vi.fn(async () => { throw new Error("denied"); }) });
		const h = await setup(provider, { activeSessions: async () => [] });
		const id = await provisioned(h.server, h.cookie, h.store);
		const failed = await deleteWorker(h.server, h.cookie, id);
		expect(failed.status).toBe(502);
		expect(h.store.getPublic(id)).toMatchObject({ status: "failed", desired: "removed", lastError: expect.any(String) });
	});
});

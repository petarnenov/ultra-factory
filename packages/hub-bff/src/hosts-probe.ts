import type { Logger } from "./log.ts";
import { HUB_ID, readHubEntry, type HostEntry, type HostsStore, type HostStatus, type PublicHost } from "./hosts-store.ts";

// The identity contract version the BFF speaks, as the CLI and desktop do.
export const REMOTE_API_VERSION = 1;
export const PROBE_TIMEOUT_MS = 5_000;
export const POLL_INTERVAL_MS = 30_000;

export type Identity = { hostId: string; apiVersion: number };
export type IdentityResult = { ok: true; identity: Identity } | { ok: false; reason: "unreachable" | "unsupported_api_version" };
export type ProbeResult = { status: HostStatus; hostId?: string };

// Redirects are never followed: a redirect must not carry the bearer to
// another address.
async function get(url: string, headers: Record<string, string> = {}): Promise<Response> {
	return fetch(url, { headers: { Accept: "application/json", ...headers }, redirect: "manual", signal: AbortSignal.timeout(PROBE_TIMEOUT_MS) });
}

// probeIdentity calls the host's unauthenticated GET /api/v1/identity.
export async function probeIdentity(baseUrl: string): Promise<IdentityResult> {
	let body: { hostId?: unknown; apiVersion?: unknown };
	try {
		const res = await get(`${baseUrl}/api/v1/identity`);
		if (res.status !== 200) {
			await res.body?.cancel();
			return { ok: false, reason: "unreachable" };
		}
		body = (await res.json()) as typeof body;
	} catch {
		return { ok: false, reason: "unreachable" };
	}
	if (typeof body?.hostId !== "string" || !body.hostId) return { ok: false, reason: "unreachable" };
	if (body.apiVersion !== REMOTE_API_VERSION) return { ok: false, reason: "unsupported_api_version" };
	return { ok: true, identity: { hostId: body.hostId, apiVersion: body.apiVersion } };
}

// checkAuth validates the bearer with an authenticated read. The expected host
// id makes the daemon refuse (421) if the address now belongs to another host.
export async function checkAuth(baseUrl: string, hostId: string, bearer: string): Promise<HostStatus> {
	try {
		const res = await get(`${baseUrl}/api/v1/projects`, { Authorization: `Bearer ${bearer}`, "X-AO-Expected-Host-ID": hostId });
		await res.body?.cancel();
		if (res.status === 200) return "online";
		if (res.status === 401 || res.status === 403) return "auth_failed";
		return "unreachable";
	} catch {
		return "unreachable";
	}
}

// probeHost runs both steps for a stored entry. A worker must still report the
// host id it was added with; the hub's loopback listener may report a new one.
export async function probeHost(entry: HostEntry): Promise<ProbeResult> {
	const identity = await probeIdentity(entry.url);
	if (!identity.ok) return { status: "unreachable" };
	const { hostId } = identity.identity;
	if (entry.source !== "hub" && hostId !== entry.hostId) return { status: "unreachable" };
	return { status: await checkAuth(entry.url, hostId, entry.password), hostId };
}

export type HostsMonitorOptions = {
	store: HostsStore;
	// Path of the hub daemon's mobile/config.json.
	hubConfigPath: string;
	log: Logger;
	intervalMs?: number;
	probe?: (entry: HostEntry) => Promise<ProbeResult>;
};

export type HostsMonitor = {
	syncHub: () => Promise<void>;
	probe: (id: string) => Promise<PublicHost | undefined>;
	pollOnce: () => Promise<void>;
	start: () => void;
	stop: () => void;
};

export function createHostsMonitor(options: HostsMonitorOptions): HostsMonitor {
	const { store, log } = options;
	const intervalMs = options.intervalMs ?? POLL_INTERVAL_MS;
	const runProbe = options.probe ?? probeHost;
	let timer: NodeJS.Timeout | undefined;
	let stopped = true;
	let warnedNotLoopback = false;

	// syncHub creates or refreshes the hub entry. The daemon writes its config
	// on first start and may rotate the password, so it runs before every poll.
	async function syncHub(): Promise<void> {
		try {
			const hub = await readHubEntry(options.hubConfigPath);
			if (!hub) return;
			if (!hub.loopbackOnly && !warnedNotLoopback) {
				warnedNotLoopback = true;
				log("warn", "hub remote-host listener is not loopback-only; ADR 0006 expects loopbackOnly in mobile/config.json");
			}
			await store.upsertHub(hub);
		} catch (err) {
			log("error", "hub entry not updated", { error: err instanceof Error ? err.message : String(err) });
		}
	}

	async function probe(id: string): Promise<PublicHost | undefined> {
		const entry = store.get(id);
		if (!entry) return undefined;
		const result = await runProbe(entry);
		const host = await store.recordProbe(entry.source === "hub" ? HUB_ID : id, result);
		if (result.status !== entry.status) log("info", "host status changed", { label: entry.label, hostId: result.hostId ?? entry.hostId, status: result.status });
		return host;
	}

	async function pollOnce(): Promise<void> {
		await syncHub();
		await Promise.all(store.list().map((host) => probe(host.id).catch(() => undefined)));
	}

	// A new round starts only after the previous one finished, so slow hosts
	// cannot pile up overlapping probes.
	function schedule(): void {
		if (stopped) return;
		timer = setTimeout(() => {
			pollOnce().finally(schedule);
		}, intervalMs);
		timer.unref();
	}

	return {
		syncHub,
		probe,
		pollOnce,
		start() {
			if (!stopped) return;
			stopped = false;
			pollOnce().finally(schedule);
		},
		stop() {
			stopped = true;
			clearTimeout(timer);
		},
	};
}

import { useQueries, useQuery } from "@tanstack/react-query";
import { isWebBuild } from "../lib/platform";
import { webHostBaseUrl } from "../lib/web-remotes";

// The hub BFF's registry as GET /hub/workers returns it: the PublicHost list
// of /hub/hosts plus the Railway reconcile facts (orphans, limit).

export type HubHostStatus = "draining" | "unknown" | "provisioning" | "online" | "unreachable" | "auth_failed" | "removing" | "failed";

export type HubHost = {
	id: string;
	label: string;
	url: string;
	hostId?: string;
	source: "manual" | "railway" | "hub";
	status: HubHostStatus;
	lastError?: string;
	createdAt: string;
	checkedAt?: string;
	removable: boolean;
	railway?: { serviceId: string; volumeId: string; environmentId: string };
};

/** A Railway service named like a worker that the registry does not know. */
export type HubOrphan = { id: string; name: string };

/** A Railway worker with no active session for at least `idleStopHours`. */
export type HubIdleWorker = { id: string; idleSince: string };

export type HubWorkers = {
	workers: HubHost[];
	orphans: HubOrphan[];
	lastReconcile?: string;
	workersByStatus: Record<string, number>;
	limit: number;
	providerConfigured?: boolean;
	idle?: HubIdleWorker[];
	idleStopHours?: number;
};

const HUB_WORKERS_PATH = "/hub/workers";
export const HUB_BUSY_POLL_MS = 5_000;
export const HUB_IDLE_POLL_MS = 30_000;

export const hubHostsQueryKey = ["hub", "workers"] as const;

// Same base as the web bridge (bridge.ts webDaemonBaseUrl): the BFF serves the
// SPA, /hub/* and /hosts/<id>/* on one origin.
export function hubBaseUrl(): string {
	return (import.meta.env.VITE_AO_API_BASE_URL ?? "").replace(/\/+$/, "");
}

/** 5 s while a worker is being provisioned or removed, otherwise 30 s. */
export function hubHostsPollInterval(data: HubWorkers | undefined): number {
	return data?.workers.some((host) => host.status === "provisioning" || host.status === "removing") ? HUB_BUSY_POLL_MS : HUB_IDLE_POLL_MS;
}

async function fetchHubWorkers(signal: AbortSignal): Promise<HubWorkers> {
	const response = await fetch(`${hubBaseUrl()}${HUB_WORKERS_PATH}`, { cache: "no-store", credentials: "same-origin", signal });
	if (!response.ok) {
		const body = (await response.json().catch(() => null)) as { error?: string; message?: string } | null;
		throw new Error(body?.message || body?.error || `Could not load workers (HTTP ${response.status})`);
	}
	const body = (await response.json()) as Partial<HubWorkers>;
	return {
		// The hub's own entry is this page's local daemon, not a worker.
		workers: (body.workers ?? []).filter((host) => host.source !== "hub"),
		orphans: body.orphans ?? [],
		...(body.lastReconcile ? { lastReconcile: body.lastReconcile } : {}),
		workersByStatus: body.workersByStatus ?? {},
		limit: body.limit ?? 0,
		...(body.providerConfigured === undefined ? {} : { providerConfigured: body.providerConfigured }),
		idle: body.idle ?? [],
		...(body.idleStopHours === undefined ? {} : { idleStopHours: body.idleStopHours }),
	};
}

async function fetchActiveSessionCount(hostId: string, signal: AbortSignal): Promise<number> {
	const response = await fetch(`${webHostBaseUrl(hubBaseUrl(), hostId)}/api/v1/sessions?active=true`, { cache: "no-store", credentials: "same-origin", signal });
	if (!response.ok) throw new Error(`Could not load sessions (HTTP ${response.status})`);
	const body = (await response.json()) as { sessions?: Array<{ isTerminated?: unknown }> };
	return (body.sessions ?? []).filter((session) => session.isTerminated === false).length;
}

/** The hub's workers and Railway orphans, polled adaptively. Web build only. */
export function useHubHosts() {
	return useQuery<HubWorkers>({
		queryKey: hubHostsQueryKey,
		enabled: isWebBuild,
		queryFn: ({ signal }) => fetchHubWorkers(signal),
		refetchInterval: (query) => hubHostsPollInterval(query.state.data),
	});
}

/** Active session counts for online and draining workers, read through the
 * BFF's /hosts/<id> proxy; a host outside those states is left out. */
export function useHubHostSessionCounts(hosts: readonly HubHost[]): Record<string, number> {
	const online = hosts.filter((host) => (host.status === "online" || host.status === "draining"));
	const results = useQueries({
		queries: online.map((host) => ({
			queryKey: ["hub", "host-sessions", host.id] as const,
			enabled: isWebBuild,
			queryFn: ({ signal }: { signal: AbortSignal }) => fetchActiveSessionCount(host.id, signal),
			refetchInterval: HUB_IDLE_POLL_MS,
		})),
	});
	const counts: Record<string, number> = {};
	online.forEach((host, index) => {
		const count = results[index]?.data;
		if (typeof count === "number") counts[host.id] = count;
	});
	return counts;
}

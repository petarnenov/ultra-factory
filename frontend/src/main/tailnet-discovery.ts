// The local daemon discovers AO hosts on the user's Tailscale network
// (GET /api/v1/tailnet/hosts); the desktop reads that list.

// It keeps ~/.ao/remotes.json in step with it and hides hosts through the
// daemon, so the CLI sees the same choices.

export type TailnetHost = {
	hostId: string;
	label: string;
	name: string;
	dnsName: string;
	address: string;
	port: number;
	url: string;
	online: boolean;
	hidden: boolean;
	lastSeen?: string;
};

export type TailnetDiscovery = {
	available: boolean;
	error?: string;
	selfLogin?: string;
	localHostIds: string[];
	hosts: TailnetHost[];
};

export type TailnetHostChanges = { hidden?: boolean; label?: string };

type FetchImpl = typeof fetch;

/** Read the daemon's tailnet directory; null when it cannot be asked. */
export async function readTailnetDiscovery(base: string | null, fetchImpl: FetchImpl = fetch): Promise<TailnetDiscovery | null> {
	if (!base) return null;
	try {
		const response = await fetchImpl(`${base}/api/v1/tailnet/hosts`, { method: "GET", signal: AbortSignal.timeout(3_000) });
		if (!response.ok) return null;
		const body = (await response.json()) as Partial<TailnetDiscovery>;
		return {
			available: body.available === true,
			error: typeof body.error === "string" ? body.error : undefined,
			selfLogin: typeof body.selfLogin === "string" ? body.selfLogin : undefined,
			localHostIds: Array.isArray(body.localHostIds) ? body.localHostIds.filter((id): id is string => typeof id === "string") : [],
			hosts: Array.isArray(body.hosts) ? body.hosts.filter((host): host is TailnetHost => typeof host?.hostId === "string" && typeof host.url === "string") : [],
		};
	} catch {
		return null;
	}
}

/** Hide, show, or relabel a discovered host on this machine's daemon. */
export async function updateTailnetHost(base: string | null, hostId: string, changes: TailnetHostChanges, fetchImpl: FetchImpl = fetch): Promise<void> {
	if (!base) throw new Error("The local AO daemon is not running.");
	const response = await fetchImpl(`${base}/api/v1/tailnet/hosts/${encodeURIComponent(hostId)}`, {
		method: "PATCH",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(changes),
		signal: AbortSignal.timeout(5_000),
	});
	if (!response.ok) throw new Error(`Could not update the host (HTTP ${response.status}).`);
}

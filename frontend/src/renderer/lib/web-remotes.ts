import type { AoBridge } from "../../preload";

// The web build's remote hosts live in the hub BFF's registry (/hub/hosts), not
// in ~/.ao/remotes.json; the BFF holds credentials and proxies /hosts/<id>.

type Remotes = AoBridge["remotes"];
type RemoteHealth = Awaited<ReturnType<Remotes["add"]>>;

// The /hub/hosts fields the renderer uses (hub-bff PublicHost).
type HubHost = {
	id: string;
	label: string;
	url: string;
	source: "manual" | "railway" | "hub";
	status: "draining" | "unknown" | "online" | "unreachable" | "auth_failed";
};

type HubError = { error?: string; message?: string };

const HUB_HOSTS_PATH = "/hub/hosts";

/** The same-origin prefix the BFF proxies a registered host under. */
export function webHostBaseUrl(apiBaseUrl: string, hostId: string): string {
	return `${apiBaseUrl}/hosts/${encodeURIComponent(hostId)}`;
}

async function readError(response: Response): Promise<HubError> {
	return ((await response.json().catch(() => null)) as HubError | null) ?? {};
}

async function failure(response: Response, fallback: string): Promise<Error> {
	const body = await readError(response);
	return new Error(body.message || body.error || `${fallback} (HTTP ${response.status})`);
}

export function createWebRemotesBridge(apiBaseUrl: () => string): Remotes {
	const hostsUrl = (suffix = "") => `${apiBaseUrl()}${HUB_HOSTS_PATH}${suffix}`;

	// The hub's own entry is the local daemon of this page, served under /api, so
	// it is not listed as a remote host.
	const listHosts = async (): Promise<HubHost[]> => {
		const response = await fetch(hostsUrl(), { cache: "no-store", credentials: "same-origin" });
		if (!response.ok) throw await failure(response, "Could not load hosts");
		const body = (await response.json()) as { hosts?: HubHost[] };
		return (body.hosts ?? []).filter((host) => host.source !== "hub");
	};

	const findHost = async (url: string, hostId?: string): Promise<HubHost> => {
		const host = (await listHosts()).find((candidate) => candidate.url === url && (!hostId || candidate.id === hostId));
		if (!host) throw new Error(`no saved host for ${url}`);
		return host;
	};

	const unsupported = async (): Promise<never> => {
		throw new Error("This action needs the desktop app.");
	};

	return {
		list: async () => (await listHosts()).map(({ id, label, url }) => ({ hostId: id, label, url, allowInsecureHTTP: false })),
		// The hub has no tailnet of its own; its hosts are registered explicitly.
		tailnet: async () => ({ available: false, hidden: [] }),
		hide: unsupported,
		unhide: unsupported,
		add: async (input): Promise<RemoteHealth> => {
			const response = await fetch(hostsUrl(), {
				method: "POST",
				credentials: "same-origin",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(input),
			});
			if (response.status === 201) return "online";
			const body = await readError(response);
			if (response.status === 422 && body.error === "host_credential_rejected") return "unauthorized";
			if (response.status === 502) return body.error === "unsupported_api_version" ? "incompatible" : "offline";
			throw new Error(body.message || body.error || `Could not add host (HTTP ${response.status})`);
		},
		// The registry has no edit route: a host is removed and added again.
		update: unsupported,
		setAllowInsecureHTTP: unsupported,
		remove: async (url) => {
			const host = await findHost(url);
			const response = await fetch(hostsUrl(`/${encodeURIComponent(host.id)}`), { method: "DELETE", credentials: "same-origin" });
			if (!response.ok && response.status !== 404) throw await failure(response, "Could not remove host");
		},
		// Connecting is probing: the BFF already proxies every paired host, so an
		// online host is usable at its prefix right away, with desktop wording.
		connect: async (url, hostId) => {
			const saved = await findHost(url, hostId);
			const response = await fetch(hostsUrl(`/${encodeURIComponent(saved.id)}/probe`), { method: "POST", credentials: "same-origin" });
			if (!response.ok) throw await failure(response, `Could not probe ${url}`);
			const { host } = (await response.json()) as { host: HubHost };
			if (host.status === "auth_failed") throw new Error(`host ${url} is unauthorized`);
			if (host.status !== "online" && host.status !== "draining") throw new Error(`host ${url} is offline`);
			return { hostId: host.id, label: host.label, url: host.url, base: webHostBaseUrl(apiBaseUrl(), host.id) };
		},
		disconnect: async () => undefined,
		// Cloud account hosts and their tokens are a desktop feature.
		importAccountHost: async () => undefined,
		pruneAccountHosts: async () => undefined,
		issueAccountToken: unsupported,
		previewUrl: async (_hostId, _sessionId, sourceUrl) => sourceUrl,
		resolvePreviewUrl: async (_hostId, _sessionId, viewedUrl) => viewedUrl,
	};
}

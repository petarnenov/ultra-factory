import { readFile } from "node:fs/promises";
import { BlockList, isIP } from "node:net";
import os from "node:os";
import path from "node:path";
import { IncompatibleRemoteVersionError, probeRemote, readRemoteIdentity, type RemoteHealth } from "./remote-request";
import { findRemote, removeSavedRemote, toHostViews, updateSavedRemote } from "./remotes-ipc";
import type { RemoteRegistry } from "./remote-registry";
import { addRemote, readRemotes, updateRemote, type RemoteChanges, type RemoteEntry } from "./remotes-store";

// An isolated desktop run must not read or write the real user's credentials.
// Without an override, keep the existing ~/.ao location.
export function remotesFilePath(): string {
	return path.join(process.env.AO_DATA_DIR?.trim() || path.join(os.homedir(), ".ao"), "remotes.json");
}

// Every AO installation on this machine keeps its identity in its data dir.
export async function readInstallationHostIds(dataDirs: string[]): Promise<string[]> {
	const ids = await Promise.all(dataDirs.map(async (dir) => {
		try {
			const { hostId } = JSON.parse(await readFile(path.join(dir, "mobile", "identity.json"), "utf8")) as { hostId?: unknown };
			return typeof hostId === "string" ? hostId : "";
		} catch { return ""; }
	}));
	return ids.filter(Boolean);
}

const TAILNET = new BlockList();
TAILNET.addSubnet("100.64.0.0", 10, "ipv4");
TAILNET.addSubnet("fd7a:115c:a1e0::", 48, "ipv6");

export const NO_TAILSCALE_ADDRESS = "This host has no Tailscale address, so it cannot be synced to your account. Install Tailscale on it, join the same tailnet, update AO there, and save the host again.";

// Accounts store only a Tailscale address; other subnets cannot reach LAN.
// Throws when the host reported none, never falling back.
export function tailnetAccountUrl(raw: unknown): string {
	const url = parseTailnetUrl(raw);
	if (!url) throw new Error(NO_TAILSCALE_ADDRESS);
	return url.origin;
}

function parseTailnetUrl(raw: unknown): URL | null {
	let url: URL;
	try { url = new URL(typeof raw === "string" ? raw : ""); } catch { return null; }
	const host = url.hostname.replace(/^\[|\]$/g, "").toLowerCase();
	const family = isIP(host);
	const tailnet = family ? TAILNET.check(host, family === 6 ? "ipv6" : "ipv4") : host.endsWith(".ts.net");
	if (!tailnet || url.protocol !== "http:" || url.username || url.password || (url.pathname !== "/" && url.pathname !== "")) return null;
	return url;
}

// The slice of Electron's ipcMain these handlers need, so tests need no Electron.
type IpcMainLike = {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any -- the listener
	// args must unify Electron's IpcMain (any[]) with a test fake (unknown[]),
	// and `never[]` rejects both; `any[]` here leaks nowhere past registration.
	handle(channel: string, listener: (event: unknown, ...args: any[]) => Promise<unknown>): void;
};

export type RemotesIpcDeps = {
	file: string;
	registry: RemoteRegistry;
	requireAccount: () => Promise<void>;
	getAccountId: () => Promise<string>;
	localIdentity: () => Promise<string | null>;
	probe?: (entry: RemoteEntry) => Promise<RemoteHealth>;
	identity?: (entry: Pick<RemoteEntry, "url">) => Promise<string>;
	// Other AO installations on this machine (dev/prod data dirs) are local too.
	siblingHostIds?: () => Promise<string[]>;
	// Loopback base URL of the attached local daemon, or null while detached.
	localDaemonUrl?: () => string | null;
};

export type LocalAccountHost = { hostId: string; label: string; url: string; token: string };

const ACCOUNT_TOKEN = /^[0-9a-f]{64}$/;

function localHostLabel(): string {
	return os.hostname().replace(/\.local$/i, "").trim().slice(0, 64) || "AO host";
}

class LocalIdentityUnavailableError extends Error {
	constructor() { super("Local AO identity is unavailable; remote connection is offline"); }
}

/**
 * Saved AO daemons, shared with the CLI's ~/.ao/remotes.json. Everything the
 * renderer receives back is password-free (see remotes-ipc.ts); the plaintext
 * password only travels renderer -> main when adding or editing a credential.
 */
export function registerRemotesIpc(
	ipcMain: IpcMainLike,
	{ file, registry, requireAccount, getAccountId, localIdentity, probe = probeRemote, identity = readRemoteIdentity, siblingHostIds = async () => [], localDaemonUrl = () => null }: RemotesIpcDeps,
): void {
	const disconnect = (url: string) => registry.disconnect(url);
	const refuseSelf = (hostId: string | undefined, localHostId: string | null, siblings: string[] = []) => {
		if (hostId && (hostId === localHostId || siblings.includes(hostId))) throw new Error("This AO installation is already available locally; a remote connection to itself is not allowed.");
	};
	const requireLocalIdentity = async () => {
		const hostId = await localIdentity();
		if (!hostId) throw new LocalIdentityUnavailableError();
		return hostId;
	};
	const checkConnectionContext = async (localHostId: string, account: string) => {
		const [currentLocalHostId, currentAccount] = await Promise.all([localIdentity(), accountID()]);
		if (currentAccount !== account) throw new Error("Account changed during remote connection.");
		if (!currentLocalHostId) throw new LocalIdentityUnavailableError();
		if (currentLocalHostId !== localHostId) throw new Error("Local AO identity changed; remote connection is offline");
	};
	const checkedProbe = async (entry: RemoteEntry, account: string): Promise<RemoteHealth> => {
		if (!entry.hostId) throw new Error("remote host must be paired again to record its identity");
		const localHostId = await requireLocalIdentity();
		const siblings = await siblingHostIds();
		refuseSelf(entry.hostId, localHostId, siblings);
		let actual: string;
		try {
			actual = await identity(entry);
		} catch (error) {
			if (error instanceof IncompatibleRemoteVersionError) return "incompatible";
			return "offline";
		}
		refuseSelf(actual, localHostId, siblings);
		if (actual !== entry.hostId) throw new Error(`remote host identity changed for ${entry.url}; connection refused`);
		await checkConnectionContext(localHostId, account);
		const health = await probe(entry);
		await checkConnectionContext(localHostId, account);
		return health;
	};
	// Keep edits and connects ordered so an update cannot leave a stale proxy.
	// ponytail: one queue includes network probes; split by host if five-host setup is too slow.
	let pending: Promise<void> = Promise.resolve();
	const ordered = <T>(operation: () => Promise<T>): Promise<T> => {
		const result = pending.then(operation, operation);
		pending = result.then(() => undefined, () => undefined);
		return result;
	};
	const accountID = async () => {
		await requireAccount();
		const id = await getAccountId();
		if (!id) throw new Error("Sign in to AO Cloud to use remote hosts.");
		return id;
	};
	const owned = async () => {
		const account = await accountID();
		const entries = await readRemotes(file);
		if (!account) return entries;
		// Legacy rows stay local-only: show them without attributing them to the
		// signed-in account, while keeping other accounts isolated.
		return entries.filter((item) => !item.accountUserId || item.accountUserId === account);
	};

	ipcMain.handle("remotes:list", () => ordered(async () => {
		await accountID();
		const local = new Set([await localIdentity(), ...await siblingHostIds()]);
		const entries = await owned();
		return toHostViews(entries.filter((entry) => !entry.hostId || !local.has(entry.hostId)));
	}));
	ipcMain.handle("remotes:add", async (_event, input: RemoteEntry) => ordered(async () => {
		const account = await accountID();
		const localHostId = await requireLocalIdentity();
		const siblings = await siblingHostIds();
		refuseSelf(input.hostId, localHostId, siblings);
		// Probe before saving: a host that never answered is worse than no host,
		// because it looks configured.
		let hostId: string;
		try {
			hostId = await identity(input);
		} catch (error) {
			if (error instanceof IncompatibleRemoteVersionError) return "incompatible" as RemoteHealth;
			return "offline" as RemoteHealth;
		}
		refuseSelf(hostId, localHostId, siblings);
		const entry = { label: input.label, url: input.url, password: input.password, hostId, accountUserId: account };
		const health = await checkedProbe(entry, account);
		if (health === "online") {
			const previous = (await owned()).find((saved) => saved.hostId === hostId);
			await addRemote(file, entry);
			if (previous && previous.url !== entry.url) await disconnect(previous.url);
			await disconnect(entry.url);
		}
		return health;
	}));
	ipcMain.handle("remotes:update", async (_event, url: string, changes: RemoteChanges) => ordered(async () => {
		const account = await accountID();
		if (!(await owned()).some((entry) => entry.url === url)) throw new Error("Host does not belong to this account.");
		const { accountUserId: _ignored, ...editable } = changes;
		return updateSavedRemote(file, url, editable, disconnect, (entry) => checkedProbe(entry, account));
	}));
	ipcMain.handle("remotes:remove", async (_event, url: string) => ordered(async () => {
		if (!(await owned()).some((entry) => entry.url === url)) throw new Error("Host does not belong to this account.");
		return removeSavedRemote(file, url, disconnect);
	}));
	ipcMain.handle("remotes:connect", async (_event, url: string, hostId?: string) => ordered(async () => {
		const account = await accountID();
		if (!(await owned()).some((entry) => entry.url === url && (!hostId || entry.hostId === hostId))) throw new Error("Host does not belong to this account.");
		const entry = await findRemote(file, url, hostId);
		const localHostId = await localIdentity();
		refuseSelf(entry.hostId, localHostId, await siblingHostIds());
		// A local restart must not retire an already verified, unchanged remote proxy.
		if (!localHostId) {
			const connected = registry.connected(entry);
			if (connected && await accountID() === account) return connected;
			throw new LocalIdentityUnavailableError();
		}
		let health: RemoteHealth;
		try { health = await checkedProbe(entry, account); }
		catch (error) {
			const connected = registry.connected(entry);
			if (error instanceof LocalIdentityUnavailableError && connected && await accountID() === account) return connected;
			throw error;
		}
		if (health !== "online") throw new Error(`host ${url} is ${health}`);
		return registry.connect(entry);
	}));
	ipcMain.handle("remotes:importAccountHost", async (_event, expectedAccountId: string, input: RemoteEntry) => ordered(async () => {
		const account = await accountID();
		if (!account || account !== expectedAccountId || !input.hostId || !ACCOUNT_TOKEN.test(input.password)) throw new Error("Invalid account host credential.");
		const local = (await readRemotes(file)).find((entry) => entry.hostId === input.hostId);
		// Keep the original password on the machine that performed pairing; a
		// Cloud refresh must not replace it with the narrower account token.
		if (local?.password && !ACCOUNT_TOKEN.test(local.password)) {
			// Move to a tailnet URL only once it answers with this hostId;
			// otherwise keep the local URL. Rotated tunnels still move.
			let url = local.url;
			if (input.url !== local.url) {
				if (!parseTailnetUrl(input.url)) url = input.url;
				else if (await identity({ url: input.url }).then((actual) => actual === input.hostId, () => false)) url = input.url;
			}
			if (local.label !== input.label || local.url !== url) await updateRemote(file, local.url, { label: input.label, url });
			if (local.url !== url) await disconnect(local.url);
			return;
		}
		await addRemote(file, { ...input, accountUserId: account });
	}));
	ipcMain.handle("remotes:pruneAccountHosts", async (_event, expectedAccountId: string, currentHostIds: string[]) => ordered(async () => {
		if (await accountID() !== expectedAccountId) throw new Error("Account changed during host sync.");
		const current = new Set(currentHostIds);
		for (const entry of await owned()) {
			// Only account-imported credentials are directory-managed. A machine
			// paired here with its original password remains a local pairing.
			if (entry.hostId && !current.has(entry.hostId) && ACCOUNT_TOKEN.test(entry.password)) {
				await removeSavedRemote(file, entry.url, disconnect);
			}
		}
	}));
	ipcMain.handle("remotes:issueAccountToken", async (_event, url: string) => ordered(async () => {
		const account = await accountID();
		const entry = (await readRemotes(file)).find((saved) => saved.url === url && (!saved.accountUserId || saved.accountUserId === account));
		if (!entry?.hostId) throw new Error("Pair this host before linking it to your account.");
		const localHostId = await requireLocalIdentity();
		refuseSelf(entry.hostId, localHostId);
		const actual = await identity(entry);
		refuseSelf(actual, localHostId);
		if (actual !== entry.hostId) throw new Error("Remote host identity changed; connection refused.");
		await checkConnectionContext(localHostId, account);
		const endpoint = new URL("/api/v1/remote-host/account-token", entry.url);
		const response = await fetch(endpoint, {
			method: "POST", redirect: "error",
			headers: { Authorization: `Bearer ${entry.password}`, "X-AO-Expected-Host-ID": entry.hostId, "X-AO-Account-ID": account },
			signal: AbortSignal.timeout(8_000),
		});
		if (!response.ok) throw new Error("The host did not accept account linking. Check its password and version.");
		const result = await response.json() as { hostId?: string; token?: string; url?: string };
		if (result.hostId !== entry.hostId || !/^[0-9a-f]{64}$/.test(result.token ?? "")) throw new Error("The host returned an invalid account credential.");
		return { token: result.token, url: tailnetAccountUrl(result.url) };
	}));
	// This machine's own daemon as an account host, so a signed-in desktop links
	// itself. Null unless remote access is on and the daemon has a tailnet URL.
	ipcMain.handle("remotes:localAccountHost", async (_event, expectedAccountId: string) => ordered(async (): Promise<LocalAccountHost | null> => {
		const account = await accountID();
		if (account !== expectedAccountId) throw new Error("Account changed during host sync.");
		const base = localDaemonUrl();
		const localHostId = await localIdentity();
		if (!base || !localHostId) return null;
		const status = await fetch(new URL("/api/v1/mobile/status", base), { redirect: "error", signal: AbortSignal.timeout(5_000) });
		if (!status.ok) return null;
		const bridge = await status.json() as { enabled?: boolean; loopbackOnly?: boolean; hostId?: string; password?: string };
		if (!bridge.enabled || bridge.loopbackOnly || bridge.hostId !== localHostId || !bridge.password) return null;
		const response = await fetch(new URL("/api/v1/remote-host/account-token", base), {
			method: "POST", redirect: "error",
			headers: { Authorization: `Bearer ${bridge.password}`, "X-AO-Expected-Host-ID": localHostId, "X-AO-Account-ID": account },
			signal: AbortSignal.timeout(5_000),
		});
		if (!response.ok) return null;
		const result = await response.json() as { hostId?: string; token?: string; url?: string };
		const url = parseTailnetUrl(result.url);
		if (result.hostId !== localHostId || !ACCOUNT_TOKEN.test(result.token ?? "") || !url) return null;
		await checkConnectionContext(localHostId, account);
		return { hostId: localHostId, label: localHostLabel(), url: url.origin, token: result.token! };
	}));
	ipcMain.handle("remotes:disconnect", async (_event, url: string) => ordered(() => disconnect(url)));
	ipcMain.handle("remotes:previewUrl", async (_event, hostId: string, sessionId: string, sourceUrl: string) => {
		const account = await accountID();
		refuseSelf(hostId, await localIdentity());
		if (await accountID() !== account) throw new Error("Account changed during remote connection.");
		return registry.previewUrl(hostId, sessionId, sourceUrl);
	});
	ipcMain.handle("remotes:resolvePreviewUrl", async (_event, hostId: string, sessionId: string, viewedUrl: string) => {
		const account = await accountID();
		refuseSelf(hostId, await localIdentity());
		if (await accountID() !== account) throw new Error("Account changed during remote connection.");
		return registry.resolvePreviewUrl(hostId, sessionId, viewedUrl);
	});
}

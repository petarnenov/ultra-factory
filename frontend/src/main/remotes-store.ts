import { randomUUID } from "node:crypto";
import { mkdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { dirname } from "node:path";

// Saved desktop connections. Older records have no hostId and must be paired
// again before a saved password can be sent to their address.

// A tailscale entry was found on the tailnet: its url is the Tailscale
// address and it has no password; the host admits this machine on identity.
export type RemoteEntry = {
	label: string;
	url: string;
	password: string;
	hostId?: string;
	accountUserId?: string;
	tailscale?: boolean;
	/** The per-host "Allow report pulls over plain HTTP" setting. */
	allowInsecureHTTP?: boolean;
};

export class RemotesFilePermissionError extends Error {
	constructor(
		readonly path: string,
		readonly mode: number,
	) {
		super(
			`${path} holds connection passwords and is readable by others (mode ${mode.toString(8).padStart(4, "0")}) — run: chmod 600 ${path}`,
		);
	}
}

function isMissing(error: unknown): boolean {
	return (error as NodeJS.ErrnoException | undefined)?.code === "ENOENT";
}

export async function readRemotes(path: string): Promise<RemoteEntry[]> {
	let mode: number;
	try {
		mode = (await stat(path)).mode & 0o777;
	} catch (error) {
		if (isMissing(error)) return [];
		throw error;
	}
	// Mirrors the CLI: a world-readable credential file is refused, not tolerated.
	// Windows is exempt for the same reason it is in the CLI (cli/remote.go:154):
	// Node reports 0o666 for every writable file there, so the check would refuse
	// every remotes.json on that platform and take saved hosts down with it.
	if (process.platform !== "win32" && mode & 0o077) throw new RemotesFilePermissionError(path, mode);

	const parsed = JSON.parse(await readFile(path, "utf8")) as { remotes?: RemoteEntry[] };
	return parsed.remotes ?? [];
}

// Write beside the existing file so interruption never truncates the only
// saved copy. The temporary file is 0600 before its contents are written.
async function writeRemotes(path: string, remotes: RemoteEntry[]): Promise<void> {
	await mkdir(dirname(path), { recursive: true, mode: 0o700 });
	const temporary = `${path}.${randomUUID()}.tmp`;
	let renamed = false;
	try {
		await writeFile(temporary, `${JSON.stringify({ remotes }, null, 2)}\n`, {
			encoding: "utf8",
			mode: 0o600,
			flag: "wx",
		});
		await rename(temporary, path);
		renamed = true;
	} finally {
		if (!renamed) await rm(temporary, { force: true });
	}
}

// Cross-process lock, also used by the daemon (remotehost/registry.go):
// a `remotes.json.lock` created exclusively; stale locks are broken.
const LOCK_WAIT_MS = 10_000;
const LOCK_STALE_MS = 30_000;
const LOCK_POLL_MS = 10;

async function withFileLock<T>(path: string, operation: () => Promise<T>): Promise<T> {
	const lock = `${path}.lock`;
	await mkdir(dirname(path), { recursive: true, mode: 0o700 });
	const deadline = Date.now() + LOCK_WAIT_MS;
	for (;;) {
		try {
			await writeFile(lock, "", { flag: "wx", mode: 0o600 });
			break;
		} catch (error) {
			if ((error as NodeJS.ErrnoException | undefined)?.code !== "EEXIST") throw error;
			const age = await stat(lock).then((info) => Date.now() - info.mtimeMs, () => 0);
			if (age > LOCK_STALE_MS) {
				await rm(lock, { force: true });
				continue;
			}
			if (Date.now() > deadline) throw new Error(`timed out waiting for ${lock}`);
			await new Promise((resolve) => setTimeout(resolve, LOCK_POLL_MS));
		}
	}
	try {
		return await operation();
	} finally {
		await rm(lock, { force: true });
	}
}

// Serialize add/update/remove so one call never overwrites another's write.
let mutationQueue: Promise<void> = Promise.resolve();

function serializeMutation<T>(operation: () => Promise<T>): Promise<T> {
	const queued = mutationQueue.then(operation, operation);
	mutationQueue = queued.then(() => undefined, () => undefined);
	return queued;
}

export async function addRemote(path: string, entry: RemoteEntry): Promise<void> {
	return serializeMutation(() => withFileLock(path, async () => {
		const existing = await readRemotes(path);
		await writeRemotes(path, [
			...existing.filter((candidate) => candidate.url !== entry.url && (!entry.hostId || candidate.hostId !== entry.hostId)),
			entry,
		]);
	}));
}

/** An edit: only the fields it carries change. */
export type RemoteChanges = Partial<RemoteEntry>;

/**
 * Absent fields keep their saved value. Written with an explicit `??` per field
 * rather than a spread because Electron's structured clone preserves a key whose
 * value is undefined — `{ password: undefined }` must not wipe a working
 * password, which is exactly what "leave blank to keep it" sends.
 */
export function applyRemoteChanges(entry: RemoteEntry, changes: RemoteChanges): RemoteEntry {
	return {
		label: changes.label ?? entry.label,
		url: changes.url ?? entry.url,
		password: changes.password ?? entry.password,
		hostId: entry.hostId,
		accountUserId: changes.accountUserId ?? entry.accountUserId,
		allowInsecureHTTP: changes.allowInsecureHTTP ?? entry.allowInsecureHTTP,
		...(entry.tailscale ? { tailscale: true } : {}),
	};
}

/** A host the daemon found on the tailnet, as the file records it. */
export type DiscoveredRemote = { hostId: string; label: string; url: string };

// Keep the file in step with tailnet discovery; returns whether it changed.
// Entries for this machine or a sibling install (dropHostIds) are removed.

// A discovered host ID already in the file moves to its Tailscale address and
// loses its password; a new one is added with the machine name as its label.

// Labels of existing entries are kept: they are the user's.
export async function reconcileTailnetRemotes(path: string, discovered: DiscoveredRemote[], dropHostIds: string[]): Promise<boolean> {
	return serializeMutation(async () => {
		const existing = await readRemotes(path);
		const drop = new Set(dropHostIds);
		let changed = false;
		const remotes = existing.filter((entry) => {
			const keep = !entry.hostId || !drop.has(entry.hostId);
			if (!keep) changed = true;
			return keep;
		});
		for (const host of discovered) {
			if (drop.has(host.hostId)) continue;
			const index = remotes.findIndex((entry) => entry.hostId === host.hostId);
			if (index < 0) {
				remotes.push({ label: host.label, url: host.url, password: "", hostId: host.hostId, tailscale: true });
				changed = true;
				continue;
			}
			const current = remotes[index];
			if (current.url === host.url && current.password === "" && current.tailscale) continue;
			remotes[index] = { ...current, url: host.url, password: "", tailscale: true };
			changed = true;
		}
		// Two rows cannot share a url: the Tailscale address wins over a stale one.
		const seen = new Set<string>();
		const deduped = remotes.filter((entry) => {
			if (seen.has(entry.url)) { changed = true; return false; }
			seen.add(entry.url);
			return true;
		});
		if (changed) await writeRemotes(path, deduped);
		return changed;
	});
}

export async function updateRemote(path: string, url: string, changes: RemoteChanges): Promise<RemoteEntry> {
	return serializeMutation(() => withFileLock(path, async () => {
		const existing = await readRemotes(path);
		const current = existing.find((candidate) => candidate.url === url);
		if (!current) throw new Error(`no saved host for ${url}`);
		const updated = applyRemoteChanges(current, changes);
		// Re-pointing a host MOVES its entry: the row keeps its place, and any other
		// row already sitting on the new url is absorbed rather than left as a twin.
		const remotes = existing
			.map((candidate) => (candidate === current ? updated : candidate))
			.filter((candidate) => candidate === updated
				|| (candidate.url !== updated.url && (!updated.hostId || candidate.hostId !== updated.hostId)));
		await writeRemotes(path, remotes);
		return updated;
	}));
}

export async function removeRemote(path: string, url: string): Promise<void> {
	return serializeMutation(() => withFileLock(path, async () => {
		const existing = await readRemotes(path);
		const remaining = existing.filter((candidate) => candidate.url !== url);
		// Removing what is not there is not an error, but it is not a write either.
		if (remaining.length === existing.length) return;
		await writeRemotes(path, remaining);
	}));
}

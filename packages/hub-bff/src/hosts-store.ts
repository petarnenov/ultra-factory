import { randomUUID } from "node:crypto";
import { mkdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { BlockList, isIP } from "node:net";
import path from "node:path";

// The registry of daemons the BFF proxies to, persisted in
// <AO_HUB_DATA_DIR>/hosts.json (ADR 0006). Each record keeps the field names of
// the desktop's remotes.json (frontend/src/main/remotes-store.ts) and the CLI's
// remoteEntry (backend/internal/cli/remote_target.go), so a record can move
// between them, and adds the hub's own bookkeeping fields.

export type HostStatus = "draining" | "unknown" | "provisioning" | "online" | "unreachable" | "auth_failed" | "removing" | "failed";
// "hub" marks the hub daemon's own entry, created from its mobile/config.json.
export type HostSource = "manual" | "railway" | "hub";

export type HostEntry = {
	label: string;
	url: string;
	// The bearer the host accepts: its connection password or account token.
	password: string;
	// Set once GET /api/v1/identity has confirmed the host. No credential is
	// sent to an entry without it.
	hostId?: string;
	accountUserId?: string;
	id?: string;
	source: HostSource;
	railway?: { serviceId: string; volumeId: string; environmentId: string };
	lastError?: string;
	desired?: "running" | "removed";
	createdAt: string;
	status: HostStatus;
	checkedAt?: string;
};

// What the browser may see: everything except the credential.
export type PublicHost = Omit<HostEntry, "password"> & { id: string; removable: boolean };

// The upstream the reverse proxy resolves a host to (ProxyHost in proxy.ts).
export type Upstream = { url: string; hostId: string; bearer: string; draining?: boolean };

export const HUB_LABEL = "hub";
// `id` used for the hub entry before its identity probe has returned a host id.
export const HUB_ID = "hub";
// The daemon's unauthenticated primary listener. ADR 0006 forbids the BFF from
// ever using it, whatever mobile/config.json says.
export const DAEMON_LOOPBACK_PORT = 3001;
const MAX_LABEL_LENGTH = 64;

export class HostsValidationError extends Error {
	readonly code: string;
	constructor(code: string, message: string) {
		super(message);
		this.code = code;
	}
}

export class HostsFilePermissionError extends Error {
	constructor(file: string, mode: number) {
		super(`${file} holds host credentials and is readable by others (mode ${mode.toString(8).padStart(4, "0")}); run: chmod 600 ${file}`);
	}
}

const loopback = new BlockList();
loopback.addSubnet("127.0.0.0", 8, "ipv4");
loopback.addSubnet("0.0.0.0", 8, "ipv4");
loopback.addAddress("::1", "ipv6");
loopback.addAddress("::", "ipv6");
loopback.addSubnet("::ffff:127.0.0.0", 104, "ipv6");
loopback.addSubnet("::ffff:0.0.0.0", 104, "ipv6");

function isLoopbackHost(hostname: string): boolean {
	// A trailing dot is the same name as an FQDN: "localhost." is localhost.
	const host = hostname.replace(/^\[|\]$/g, "").replace(/\.+$/, "").toLowerCase();
	if (host === "localhost" || host.endsWith(".localhost")) return true;
	const family = isIP(host);
	return family !== 0 && loopback.check(host, family === 6 ? "ipv6" : "ipv4");
}

// normalizeHostUrl accepts only an http(s) origin, without credentials, path,
// query, or fragment (the proxy forwards to origins only), and returns it
// without a trailing slash. Manual hosts may not
// point at loopback: on the hub that is the daemon itself, and the only
// loopback upstream allowed is the hub entry built from mobile/config.json.
export function normalizeHostUrl(raw: unknown, allowLoopback = false): string {
	if (typeof raw !== "string" || !raw.trim()) throw new HostsValidationError("invalid_url", "url is required");
	let url: URL;
	try {
		url = new URL(raw.trim());
	} catch {
		throw new HostsValidationError("invalid_url", "url must be an absolute http:// or https:// URL");
	}
	if (url.protocol !== "http:" && url.protocol !== "https:") throw new HostsValidationError("invalid_url", "url must use http:// or https://");
	if (url.username || url.password) throw new HostsValidationError("invalid_url", "url must not contain credentials");
	if (url.pathname !== "/" || url.search || url.hash) throw new HostsValidationError("invalid_url", "url must be an origin without a path, query, or fragment");
	if (!allowLoopback && isLoopbackHost(url.hostname)) throw new HostsValidationError("invalid_url", "url must not point at a loopback address");
	return url.origin;
}

function normalizeLabel(raw: unknown): string {
	const label = typeof raw === "string" ? raw.trim() : "";
	if (!label) throw new HostsValidationError("invalid_label", "label is required");
	if (label.length > MAX_LABEL_LENGTH) throw new HostsValidationError("invalid_label", `label must be at most ${MAX_LABEL_LENGTH} characters`);
	if (/[\u0000-\u001f\u007f]/.test(label)) throw new HostsValidationError("invalid_label", "label must not contain control characters");
	return label;
}

function idOf(entry: HostEntry): string {
	return entry.id ?? entry.hostId ?? (entry.source === "hub" ? HUB_ID : "");
}

function toPublic(entry: HostEntry): PublicHost {
	const { password: _password, ...rest } = entry;
	return { id: idOf(entry), ...rest, removable: entry.source !== "hub" };
}

function isMissing(error: unknown): boolean {
	return (error as NodeJS.ErrnoException | undefined)?.code === "ENOENT";
}

export type NewHost = {
	label: unknown;
	url: unknown;
	password: unknown;
	hostId: string;
	accountUserId?: string;
	source?: "manual" | "railway";
	railway?: HostEntry["railway"];
	status?: HostStatus;
};

export class HostsStore {
	readonly file: string;
	readonly #allowLoopback: boolean;
	readonly #now: () => number;
	#hosts: HostEntry[] = [];
	#queue: Promise<void> = Promise.resolve();

	private constructor(file: string, allowLoopback: boolean, now: () => number) {
		this.file = file;
		this.#allowLoopback = allowLoopback;
		this.#now = now;
	}

	// open loads <dataDir>/hosts.json, creating nothing until the first write.
	// It refuses a file readable by group or others, as the desktop does.
	// allowLoopback is for tests whose fake hosts listen on 127.0.0.1.
	static async open(dataDir: string, options: { allowLoopback?: boolean; now?: () => number } = {}): Promise<HostsStore> {
		const store = new HostsStore(path.join(dataDir, "hosts.json"), options.allowLoopback ?? false, options.now ?? Date.now);
		await store.#load();
		return store;
	}

	async #load(): Promise<void> {
		let mode: number;
		try {
			mode = (await stat(this.file)).mode & 0o777;
		} catch (error) {
			if (isMissing(error)) return;
			throw error;
		}
		if (mode & 0o077) throw new HostsFilePermissionError(this.file, mode);
		const parsed: unknown = JSON.parse(await readFile(this.file, "utf8"));
		if (!Array.isArray(parsed)) throw new Error(`${this.file} must contain a JSON array of hosts`);
		this.#hosts = parsed.map((raw: Partial<HostEntry>) => ({
			...raw,
			label: String(raw.label ?? ""),
			url: String(raw.url ?? ""),
			password: String(raw.password ?? ""),
			source: raw.source ?? "manual",
			createdAt: raw.createdAt ?? new Date(this.#now()).toISOString(),
			status: raw.status ?? "unknown",
			...(raw.id ? { id: raw.id } : {}),
		}));
	}

	// Write beside the existing file so an interruption never truncates the
	// only copy. The temporary file is 0600 before any content is written.
	async #save(hosts: HostEntry[]): Promise<void> {
		await mkdir(path.dirname(this.file), { recursive: true, mode: 0o700 });
		const temporary = `${this.file}.${randomUUID()}.tmp`;
		let renamed = false;
		try {
			await writeFile(temporary, `${JSON.stringify(hosts, null, 2)}\n`, { encoding: "utf8", mode: 0o600, flag: "wx" });
			await rename(temporary, this.file);
			renamed = true;
		} finally {
			if (!renamed) await rm(temporary, { force: true });
		}
		this.#hosts = hosts;
	}

	// Mutations are serialized so concurrent requests cannot lose each other's writes.
	#mutate<T>(operation: (hosts: HostEntry[]) => Promise<T>): Promise<T> {
		const run = this.#queue.then(() => operation(this.#hosts.map((h) => ({ ...h }))));
		this.#queue = run.then(
			() => undefined,
			() => undefined,
		);
		return run;
	}

	list(): PublicHost[] {
		return this.#hosts.map(toPublic);
	}

	listRailwayWorkers(): HostEntry[] {
		return this.#hosts.filter((host) => host.source === "railway").map((host) => ({ ...host, ...(host.railway ? { railway: { ...host.railway } } : {}) }));
	}

	// get finds an entry by its host id, or the hub entry by "hub".
	get(id: string): HostEntry | undefined {
		const entry = id === HUB_ID ? this.#hosts.find((h) => h.source === "hub") : this.#hosts.find((h) => h.hostId === id || h.id === id);
		return entry && { ...entry };
	}

	getPublic(id: string): PublicHost | undefined {
		const entry = this.get(id);
		return entry && toPublic(entry);
	}

	// getHost and getHub implement the reverse proxy's ProxyHosts. They answer
	// only for an entry whose identity has been confirmed, so a credential is
	// never sent to an unpaired address (ADR 0006).
	getHost(id: string): Upstream | undefined {
		const entry = this.get(id);
		if (!entry?.hostId || !entry.password) return undefined;
		return { url: entry.url, hostId: entry.hostId, bearer: entry.password, ...(entry.status === "draining" ? { draining: true } : {}) };
	}

	getHub(): Upstream | undefined {
		return this.getHost(HUB_ID);
	}

	// labelTaken lets the API refuse a duplicate before probing the address.
	labelTaken(label: unknown): boolean {
		const wanted = normalizeLabel(label).toLowerCase();
		return wanted === HUB_LABEL || this.#hosts.some((h) => h.label.toLowerCase() === wanted);
	}

	async add(input: NewHost): Promise<PublicHost> {
		const label = normalizeLabel(input.label);
		const url = normalizeHostUrl(input.url, this.#allowLoopback);
		if (typeof input.password !== "string" || !input.password) throw new HostsValidationError("invalid_password", "password is required");
		if (!input.hostId) throw new HostsValidationError("invalid_host_id", "hostId is required");
		const password = input.password;
		return this.#mutate(async (hosts) => {
			if (label.toLowerCase() === HUB_LABEL || hosts.some((h) => h.label.toLowerCase() === label.toLowerCase())) {
				throw new HostsValidationError("label_taken", `a host labelled "${label}" already exists`);
			}
			if (hosts.some((h) => h.hostId === input.hostId)) throw new HostsValidationError("host_exists", "this host is already registered");
			const nowIso = new Date(this.#now()).toISOString();
			const entry: HostEntry = {
				label,
				url,
				password,
				hostId: input.hostId,
				...(input.accountUserId ? { accountUserId: input.accountUserId } : {}),
				source: input.source ?? "manual",
				...(input.railway ? { railway: input.railway } : {}),
				createdAt: nowIso,
				status: input.status ?? "unknown",
				...(input.status ? { checkedAt: nowIso } : {}),
			};
			await this.#save([...hosts, entry]);
			return toPublic(entry);
		});
	}

	// createWorker writes its intent before any provider call, making provisioning recoverable after a crash.
	async createWorker(input: { id: string; label: unknown; password: string; maxWorkers?: number }): Promise<PublicHost> {
		const label = normalizeLabel(input.label);
		return this.#mutate(async (hosts) => {
			const workerCount = hosts.filter((host) => host.source === "railway").length;
			if (input.maxWorkers !== undefined && workerCount >= input.maxWorkers) {
				throw new HostsValidationError("worker_limit_reached", `worker limit reached (${workerCount}/${input.maxWorkers})`);
			}
			if (label.toLowerCase() === HUB_LABEL || hosts.some((h) => h.label.toLowerCase() === label.toLowerCase())) {
				throw new HostsValidationError("label_taken", `a host labelled "${label}" already exists`);
			}
			const entry: HostEntry = {
				id: input.id, label, url: "", password: input.password, source: "railway",
				createdAt: new Date(this.#now()).toISOString(), status: "provisioning", desired: "running",
			};
			await this.#save([...hosts, entry]);
			return toPublic(entry);
		});
	}

	async updateWorker(id: string, update: Partial<Pick<HostEntry, "url" | "hostId" | "railway" | "status" | "lastError" | "desired">>): Promise<PublicHost | undefined> {
		return this.#mutate(async (hosts) => {
			const index = hosts.findIndex((h) => h.id === id || h.hostId === id);
			if (index < 0) return undefined;
			const next = { ...hosts[index], ...update };
			hosts[index] = next;
			await this.#save(hosts);
			return toPublic(next);
		});
	}

	async removeWorker(id: string): Promise<boolean> {
		return this.#mutate(async (hosts) => {
			const index = hosts.findIndex((h) => h.id === id || h.hostId === id);
			if (index < 0) return false;
			if (hosts[index].source !== "railway") throw new HostsValidationError("not_railway_worker", "host is not a Railway worker");
			await this.#save(hosts.filter((_, i) => i !== index));
			return true;
		});
	}

	// remove deletes only the registry record. It never touches Railway.
	async remove(id: string): Promise<boolean> {
		return this.#mutate(async (hosts) => {
			const index = hosts.findIndex((h) => (id === HUB_ID ? h.source === "hub" : h.hostId === id || h.id === id));
			if (index < 0) return false;
			if (hosts[index].source === "hub") throw new HostsValidationError("hub_not_removable", "the hub entry cannot be removed");
			await this.#save(hosts.filter((_, i) => i !== index));
			return true;
		});
	}

	// upsertHub writes the hub's own entry, refreshing its address and bearer
	// from mobile/config.json, which the daemon may rotate. Changing either
	// clears the confirmed host id so it is paired again before use.
	async upsertHub(hub: { url: string; password: string }): Promise<void> {
		return this.#mutate(async (hosts) => {
			const index = hosts.findIndex((h) => h.source === "hub");
			const current = index >= 0 ? hosts[index] : undefined;
			if (current && current.url === hub.url && current.password === hub.password) return;
			const next: HostEntry = {
				label: HUB_LABEL,
				url: hub.url,
				password: hub.password,
				...(current?.url === hub.url && current.hostId ? { hostId: current.hostId } : {}),
				source: "hub",
				createdAt: current?.createdAt ?? new Date(this.#now()).toISOString(),
				status: "unknown",
			};
			// The hub stays first so listings show it ahead of the workers.
			await this.#save([next, ...hosts.filter((_, i) => i !== index)]);
		});
	}

	// recordProbe stores a probe result. The file is rewritten only when the
	// status or the hub's host id changes, not on every poll.
	async recordProbe(id: string, result: { status: HostStatus; hostId?: string }): Promise<PublicHost | undefined> {
		return this.#mutate(async (hosts) => {
			const index = hosts.findIndex((h) => (id === HUB_ID ? h.source === "hub" : h.hostId === id || h.id === id));
			if (index < 0) return undefined;
			const entry = hosts[index];
			const checkedAt = new Date(this.#now()).toISOString();
			// Only the hub adopts the host id it reports: its address is the
			// container's own loopback listener. A worker keeps the id it was
			// added with, and a different answer is a failed probe.
			const hostId = entry.source === "hub" && result.hostId ? result.hostId : entry.hostId;
			const next: HostEntry = { ...entry, status: entry.status === "draining" ? "draining" : result.status, checkedAt, ...(hostId ? { hostId } : {}) };
			if (next.status !== entry.status || next.hostId !== entry.hostId) {
				hosts[index] = next;
				await this.#save(hosts);
			} else {
				this.#hosts[index] = next;
			}
			return toPublic(next);
		});
	}
}

// readHubEntry derives the hub's own upstream from the daemon's
// <AO_DATA_DIR>/mobile/config.json on the same volume (ADR 0006, variant B2):
// the authenticated listener on loopback at its lastPort, with the account
// token as the bearer when there is one. It returns undefined while the
// listener is not enabled yet, for example before the daemon's first start.
export async function readHubEntry(configPath: string): Promise<{ url: string; password: string; loopbackOnly: boolean } | undefined> {
	let raw: string;
	try {
		raw = await readFile(configPath, "utf8");
	} catch (error) {
		if (isMissing(error)) return undefined;
		throw error;
	}
	const config = JSON.parse(raw) as { enabled?: unknown; password?: unknown; accountToken?: unknown; lastPort?: unknown; loopbackOnly?: unknown };
	if (config.enabled !== true) return undefined;
	const bearer = typeof config.accountToken === "string" && config.accountToken ? config.accountToken : config.password;
	if (typeof bearer !== "string" || !bearer) return undefined;
	const port = config.lastPort === undefined || config.lastPort === 0 ? 3011 : Number(config.lastPort);
	if (!Number.isInteger(port) || port <= 0 || port > 65535) throw new Error(`${configPath}: lastPort is not a TCP port`);
	if (port === DAEMON_LOOPBACK_PORT) throw new Error(`${configPath}: lastPort ${port} is the daemon's unauthenticated listener; the BFF must use the authenticated one`);
	return { url: `http://127.0.0.1:${port}`, password: bearer, loopbackOnly: config.loopbackOnly === true };
}

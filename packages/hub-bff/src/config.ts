import { existsSync } from "node:fs";
import { homedir } from "node:os";
import path from "node:path";
import { hashPassword, parsePasswordHash } from "./password.ts";
import { parseTailnetSocks, type TailnetSocks } from "./tailnet.ts";

export type Config = {
	host: string;
	port: number;
	spaDir: string;
	passwordHash: string;
	publicOrigin?: string;
	plaintextPassword: boolean;
	// Directory holding hosts.json, on the hub's volume.
	hubDataDir: string;
	// The hub daemon's mobile/config.json, source of the "hub" host entry.
	hubConfigPath: string;
	railwayToken?: string;
	railwayProjectId?: string;
	railwayEnvironmentId?: string;
	workerImage?: string;
	workerUrlMode: "public" | "private";
	providerMode?: "fake";
	fakeWorkerUrl?: string;
	fakeWorkerPassword?: string;
	maxWorkers: number;
	// Largest worker volume a POST /hub/workers spec may request, in GB.
	maxVolumeGb?: number;
	// Hours with no active session before a worker is flagged idle; unset is off.
	idleStopHours?: number;
	// ADR 0008 loopback SOCKS5 for tailnet destinations; unset dials directly.
	tailnetSocks?: TailnetSocks;
};

// loadConfig reads the BFF's environment. It throws with an operator-facing
// message (never containing a secret) when the configuration is unusable.
export async function loadConfig(env: NodeJS.ProcessEnv): Promise<Config> {
	const port = Number(env.PORT ?? "8080");
	if (!Number.isInteger(port) || port <= 0 || port > 65535) throw new Error("PORT must be a TCP port number");

	const spaDir = env.AO_HUB_SPA_DIR ? path.resolve(env.AO_HUB_SPA_DIR) : "";
	if (!spaDir || !existsSync(path.join(spaDir, "index.html"))) {
		throw new Error("AO_HUB_SPA_DIR must point at the web build (frontend/dist-web) containing index.html");
	}

	const hash = env.AO_WEB_PASSWORD_HASH?.trim();
	const plaintext = env.AO_WEB_PASSWORD;
	if (hash && plaintext) throw new Error("set only one of AO_WEB_PASSWORD_HASH and AO_WEB_PASSWORD");
	let passwordHash: string;
	if (hash) {
		parsePasswordHash(hash);
		passwordHash = hash;
	} else if (plaintext) {
		passwordHash = await hashPassword(plaintext);
	} else {
		throw new Error("AO_WEB_PASSWORD_HASH (or AO_WEB_PASSWORD) is required");
	}

	let publicOrigin: string | undefined;
	if (env.AO_WEB_ORIGIN) {
		const url = new URL(env.AO_WEB_ORIGIN);
		if (url.origin !== env.AO_WEB_ORIGIN.replace(/\/$/, "")) throw new Error("AO_WEB_ORIGIN must be an origin like https://hub.example.com");
		publicOrigin = url.origin;
	}

	// Defaults follow the daemon's own: AO_DATA_DIR, else ~/.ao/data.
	const daemonDataDir = path.resolve(env.AO_DATA_DIR || path.join(homedir(), ".ao", "data"));
	const hubDataDir = path.resolve(env.AO_HUB_DATA_DIR || daemonDataDir);
	const hubConfigPath = path.resolve(env.AO_HUB_DAEMON_PASSWORD_FILE || path.join(daemonDataDir, "mobile", "config.json"));
	const workerUrlMode = env.AO_HUB_WORKER_URL_MODE ?? "public";
	if (workerUrlMode !== "public" && workerUrlMode !== "private") throw new Error("AO_HUB_WORKER_URL_MODE must be public or private");
	const maxWorkers = Number(env.AO_HUB_MAX_WORKERS ?? "5");
	if (!Number.isSafeInteger(maxWorkers) || maxWorkers < 1) throw new Error("AO_HUB_MAX_WORKERS must be a positive integer");
	const maxVolumeGb = optionalPositiveNumber(env.AO_HUB_MAX_VOLUME_GB, "AO_HUB_MAX_VOLUME_GB");
	const idleStopHours = optionalPositiveNumber(env.AO_HUB_IDLE_STOP_HOURS, "AO_HUB_IDLE_STOP_HOURS");
	const providerMode = env.AO_HUB_PROVIDER;
	if (providerMode && providerMode !== "fake") throw new Error("AO_HUB_PROVIDER must be fake when set");
	if (providerMode === "fake" && (env.RAILWAY_PROJECT_ID || env.RAILWAY_ENVIRONMENT_ID)) throw new Error("AO_HUB_PROVIDER=fake cannot be used with Railway project or environment IDs");
	if (providerMode === "fake" && (!env.AO_HUB_FAKE_WORKER_URL || !env.AO_HUB_FAKE_WORKER_PASSWORD)) throw new Error("AO_HUB_FAKE_WORKER_URL and AO_HUB_FAKE_WORKER_PASSWORD are required when AO_HUB_PROVIDER=fake");
	// Railway injects RAILWAY_PROJECT_ID and RAILWAY_ENVIRONMENT_ID into every
	// service, so both IDs are normal; only a token without them is a mistake.
	if (env.RAILWAY_API_TOKEN && !(env.RAILWAY_PROJECT_ID && env.RAILWAY_ENVIRONMENT_ID)) throw new Error("RAILWAY_API_TOKEN needs RAILWAY_PROJECT_ID and RAILWAY_ENVIRONMENT_ID");

	const tailnetSocks = parseTailnetSocks(env.AO_REMOTE_TAILNET_SOCKS);

	return {
		host: env.AO_HUB_BFF_HOST ?? "::", port, spaDir, passwordHash, publicOrigin, plaintextPassword: !hash, hubDataDir, hubConfigPath,
		railwayToken: env.RAILWAY_API_TOKEN, railwayProjectId: env.RAILWAY_PROJECT_ID, railwayEnvironmentId: env.RAILWAY_ENVIRONMENT_ID,
		workerImage: env.AO_HUB_WORKER_IMAGE, workerUrlMode, maxWorkers, maxVolumeGb, idleStopHours,
		...(tailnetSocks ? { tailnetSocks } : {}),
		...(providerMode === "fake" ? { providerMode, fakeWorkerUrl: env.AO_HUB_FAKE_WORKER_URL, fakeWorkerPassword: env.AO_HUB_FAKE_WORKER_PASSWORD } : {}),
	};
}

// optionalPositiveNumber parses an unset-or-positive numeric variable. An unset
// or empty value means "no limit" / "off".
function optionalPositiveNumber(value: string | undefined, name: string): number | undefined {
	if (value === undefined || value.trim() === "") return undefined;
	const parsed = Number(value);
	if (!Number.isFinite(parsed) || parsed <= 0) throw new Error(`${name} must be a positive number`);
	return parsed;
}

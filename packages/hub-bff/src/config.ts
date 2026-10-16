import { existsSync } from "node:fs";
import { homedir } from "node:os";
import path from "node:path";
import { hashPassword, parsePasswordHash } from "./password.ts";

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

	return {
		host: env.AO_HUB_BFF_HOST ?? "::", port, spaDir, passwordHash, publicOrigin, plaintextPassword: !hash, hubDataDir, hubConfigPath,
		railwayToken: env.RAILWAY_API_TOKEN, railwayProjectId: env.RAILWAY_PROJECT_ID, railwayEnvironmentId: env.RAILWAY_ENVIRONMENT_ID,
		workerImage: env.AO_HUB_WORKER_IMAGE, workerUrlMode,
	};
}

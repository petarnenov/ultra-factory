import { existsSync } from "node:fs";
import path from "node:path";
import { hashPassword, parsePasswordHash } from "./password.ts";

export type Config = {
	host: string;
	port: number;
	spaDir: string;
	passwordHash: string;
	publicOrigin?: string;
	plaintextPassword: boolean;
};

// loadConfig чете env-а на BFF-а. Ако конфигурацията е неизползваема, хвърля
// грешка със съобщение за оператора (никога със секрет в него).
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

	return { host: env.AO_HUB_BFF_HOST ?? "::", port, spaDir, passwordHash, publicOrigin, plaintextPassword: !hash };
}

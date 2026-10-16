import { loadConfig } from "./config.ts";
import { jsonLogger } from "./log.ts";
import { createBff } from "./server.ts";

const log = jsonLogger();

try {
	const config = await loadConfig(process.env);
	// Паролата в чист текст вече е хеширана; махни я от env-а на процеса.
	delete process.env.AO_WEB_PASSWORD;
	const server = createBff({ spaDir: config.spaDir, passwordHash: config.passwordHash, publicOrigin: config.publicOrigin, log });
	server.listen(config.port, config.host, () => {
		log("info", "hub bff listening", {
			host: config.host,
			port: config.port,
			spaDir: config.spaDir,
			passwordSource: config.plaintextPassword ? "AO_WEB_PASSWORD" : "AO_WEB_PASSWORD_HASH",
		});
	});
	for (const signal of ["SIGTERM", "SIGINT"] as const) {
		process.once(signal, () => {
			log("info", "hub bff shutting down", { signal });
			server.close(() => process.exit(0));
			server.closeAllConnections();
		});
	}
} catch (err) {
	log("error", "hub bff failed to start", { error: err instanceof Error ? err.message : String(err) });
	process.exit(1);
}

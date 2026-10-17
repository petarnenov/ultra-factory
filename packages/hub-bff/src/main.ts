import { loadConfig } from "./config.ts";
import { createHostsApi } from "./hosts-api.ts";
import { createHostsMonitor } from "./hosts-probe.ts";
import { HostsStore } from "./hosts-store.ts";
import { jsonLogger } from "./log.ts";
import { createBff } from "./server.ts";
import { RailwayClient } from "./railway-client.ts";
import { FakeRailwayProvider } from "./fake-provider.ts";
import { createWorkersApi } from "./workers-api.ts";
import { openAuditLog } from "./audit.ts";
import path from "node:path";

const log = jsonLogger();

try {
	const config = await loadConfig(process.env);
	// The plaintext password has been hashed; drop it from this process's env.
	delete process.env.AO_WEB_PASSWORD;
	const store = await HostsStore.open(config.hubDataDir);
	const audit = await openAuditLog(path.join(config.hubDataDir, "audit.jsonl"));
	const monitor = createHostsMonitor({ store, hubConfigPath: config.hubConfigPath, log });
	await monitor.syncHub();
	const hostsApi = createHostsApi({ store, monitor, log, audit });
	const provider = config.providerMode === "fake"
		? new FakeRailwayProvider(config.fakeWorkerUrl!)
		: config.railwayToken && config.railwayProjectId && config.railwayEnvironmentId
		? new RailwayClient({ token: config.railwayToken, projectId: config.railwayProjectId, environmentId: config.railwayEnvironmentId })
		: undefined;
	if (config.providerMode === "fake") log("info", "fake worker provider enabled");
	if (provider instanceof RailwayClient) await provider.assertProjectScope();
	const workersApi = createWorkersApi({ store, provider, image: config.workerImage, urlMode: config.workerUrlMode, maxWorkers: config.maxWorkers, log, audit, ...(config.providerMode === "fake" ? { workerPassword: () => config.fakeWorkerPassword! } : {}) });
	const server = createBff({ spaDir: config.spaDir, passwordHash: config.passwordHash, publicOrigin: config.publicOrigin, log, hosts: store, hostsApi, workersApi, audit });
	server.listen(config.port, config.host, () => {
		log("info", "hub bff listening", {
			host: config.host,
			port: config.port,
			spaDir: config.spaDir,
			passwordSource: config.plaintextPassword ? "AO_WEB_PASSWORD" : "AO_WEB_PASSWORD_HASH",
			hostsFile: store.file,
		});
		monitor.start();
		workersApi.start();
	});
	for (const signal of ["SIGTERM", "SIGINT"] as const) {
		process.once(signal, () => {
			log("info", "hub bff shutting down", { signal });
			monitor.stop();
			workersApi.stop();
			server.close(() => process.exit(0));
			server.closeAllConnections();
		});
	}
} catch (err) {
	log("error", "hub bff failed to start", { error: err instanceof Error ? err.message : String(err) });
	process.exit(1);
}

import { describe, expect, it, vi } from "vitest";
import {
	RailwayAuthError,
	RailwayClient,
	RailwayNotFound,
	RailwayQuotaError,
	railwayQueries,
} from "../src/railway-client.ts";

const options = {
	token: "masked-test-token",
	projectId: "<PROJECT_ID>",
	environmentId: "<ENVIRONMENT_ID>",
	retryDelayMs: 0,
};

function reply(body: unknown, status = 200): Response {
	return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("RailwayClient", () => {
	it("creates service and volume from the recorded successful response shapes", async () => {
		const fetch = vi.fn<typeof globalThis.fetch>()
			.mockResolvedValueOnce(reply({ data: { serviceCreate: { id: "<SERVICE_ID>", name: "worker" } } }))
			.mockResolvedValueOnce(reply({ data: { volumeCreate: { id: "<VOLUME_ID>", name: "worker-volume" } } }))
			.mockResolvedValueOnce(reply({ data: { serviceDomainCreate: { id: "<DOMAIN_ID>", domain: "worker.up.railway.app" } } }))
			.mockResolvedValueOnce(reply({ data: { variableUpsert: true } }))
			.mockResolvedValueOnce(reply({ data: { serviceInstanceRedeploy: "<DEPLOYMENT_ID>" } }));
		const client = new RailwayClient({ ...options, fetch });

		await expect(client.create({ name: "worker", image: "example/worker:latest", env: { AO_WORKER: "true" }, volumeMountPath: "/data" }))
			.resolves.toEqual({ serviceId: "<SERVICE_ID>", volumeId: "<VOLUME_ID>", environmentId: options.environmentId, url: "https://worker.up.railway.app" });
		expect(fetch).toHaveBeenCalledTimes(5);
		const firstRequest = JSON.parse(String(fetch.mock.calls[0]?.[1]?.body)) as { query: string; variables: { input: { source: { image: string } } } };
		expect(firstRequest.query).toBe(railwayQueries.createService);
		expect(firstRequest.variables.input.source.image).toBe("example/worker:latest");
		expect(fetch.mock.calls[0]?.[1]?.headers).toMatchObject({ "Project-Access-Token": options.token });
	});

	it("uses Bearer auth for account tokens", async () => {
		const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(reply({ data: { variableUpsert: true } }));
		const client = new RailwayClient({ ...options, tokenKind: "account", fetch });

		await client.setVariables("<SERVICE_ID>", { FLAG: "yes" });
		expect(fetch.mock.calls[0]?.[1]?.headers).toMatchObject({ Authorization: `Bearer ${options.token}` });
	});

	it("lists project services using the documented project services connection", async () => {
		const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(reply({ data: { project: { services: { edges: [
			{ node: { id: "service-1", name: "ao-worker-one" } },
			{ node: { id: "service-2", name: "api" } },
		] } } } }));
		const client = new RailwayClient({ ...options, fetch });

		await expect(client.list()).resolves.toEqual([
			{ id: "service-1", name: "ao-worker-one" },
			{ id: "service-2", name: "api" },
		]);
		const request = JSON.parse(String(fetch.mock.calls[0]?.[1]?.body)) as { query: string; variables: { projectId: string } };
		expect(request.query).toBe(railwayQueries.listServices);
		expect(request.variables.projectId).toBe(options.projectId);
	});

	it("retries 429 and 5xx responses", async () => {
		const fetch = vi.fn<typeof globalThis.fetch>()
			.mockResolvedValueOnce(new Response("{}", { status: 429, headers: { "Retry-After": "2" } }))
			.mockResolvedValueOnce(reply({}, 503))
			.mockResolvedValueOnce(reply({ data: { variableUpsert: true } }));
		const sleep = vi.fn(async () => undefined);
		const client = new RailwayClient({ ...options, fetch, sleep });

		await expect(client.setVariables("<SERVICE_ID>", { FLAG: "yes" })).resolves.toBeUndefined();
		expect(fetch).toHaveBeenCalledTimes(3);
		expect(sleep.mock.calls).toEqual([[2000], [0]]);
	});

	it("maps HTTP and GraphQL authorization failures without including response text", async () => {
		const httpFetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(reply({ message: options.token }, 401));
		const httpClient = new RailwayClient({ ...options, fetch: httpFetch });
		await expect(httpClient.getStatus("<SERVICE_ID>")).rejects.toBeInstanceOf(RailwayAuthError);
		await expect(httpClient.getStatus("<SERVICE_ID>")).rejects.not.toThrow(options.token);

		const gqlFetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(reply({ errors: [{ message: "Not Authorized", path: ["serviceDelete"] }], data: null }));
		const gqlClient = new RailwayClient({ ...options, fetch: gqlFetch });
		await expect(gqlClient.delete("<SERVICE_ID>", "<VOLUME_ID>")).rejects.toBeInstanceOf(RailwayAuthError);
		expect(gqlFetch).toHaveBeenCalledTimes(1);
	});

	it("maps quota responses and nonexistent services", async () => {
		const quotaFetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(reply({ errors: [{ message: "Project volume limit exceeded" }], data: null }));
		const quotaClient = new RailwayClient({ ...options, fetch: quotaFetch });
		await expect(quotaClient.create({ name: "worker", image: "example/worker", env: {}, volumeMountPath: "/data" })).rejects.toBeInstanceOf(RailwayQuotaError);

		const missingFetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(reply({ data: { serviceInstance: null } }));
		const missingClient = new RailwayClient({ ...options, fetch: missingFetch });
		await expect(missingClient.getStatus("<SERVICE_ID>")).rejects.toBeInstanceOf(RailwayNotFound);
	});

	it.each([
		["SUCCESS", "running"],
		["FAILED", "failed"],
		["CRASHED", "failed"],
		["REMOVED", "stopped"],
		["DEPLOYING", "provisioning"],
	] as const)("maps deployment status %s to %s", async (status, expected) => {
		const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(reply({ data: { serviceInstance: { latestDeployment: { status } } } }));
		const client = new RailwayClient({ ...options, fetch });
		await expect(client.getStatus("<SERVICE_ID>")).resolves.toBe(expected);
	});

	it("maps recorded delete authorization errors rather than treating them as success", async () => {
		const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(reply({ errors: [{ message: "Not Authorized", path: ["serviceDelete"], extensions: { code: "INTERNAL_SERVER_ERROR" } }], data: null }));
		const client = new RailwayClient({ ...options, fetch });
		await expect(client.delete("<SERVICE_ID>", "<VOLUME_ID>")).rejects.toBeInstanceOf(RailwayAuthError);
	});

	it("maps the recorded volume deletion authorization error", async () => {
		const fetch = vi.fn<typeof globalThis.fetch>()
			.mockResolvedValueOnce(reply({ data: { serviceDelete: true } }))
			.mockResolvedValueOnce(reply({ errors: [{ message: "Not Authorized", path: ["volumeDelete"], extensions: { code: "INTERNAL_SERVER_ERROR" } }], data: null }));
		const client = new RailwayClient({ ...options, fetch });
		await expect(client.delete("<SERVICE_ID>", "<VOLUME_ID>")).rejects.toBeInstanceOf(RailwayAuthError);
		expect(fetch).toHaveBeenCalledTimes(2);
	});
});

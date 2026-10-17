import { describe, expect, it } from "vitest";
import { FakeRailwayProvider } from "../src/fake-provider.ts";

describe("FakeRailwayProvider", () => {
	it("creates one in-memory worker without external resources and deletes it", async () => {
		const provider = new FakeRailwayProvider("http://worker-1:3011");
		const spec = { name: "ao-worker-test", image: "ao-worker:e2e", env: {}, volumeMountPath: "/data" as const };
		const created = await provider.create(spec);
		expect(created).toEqual({ serviceId: "fake-1", volumeId: "fake-vol-1", environmentId: "fake-env", url: "http://worker-1:3011" });
		expect(await provider.getStatus(created.serviceId)).toBe("running");
		expect(await provider.list()).toEqual([{ id: "fake-1", name: spec.name }]);
		await expect(provider.create(spec)).rejects.toThrow(/one worker/);
		await provider.setVariables(created.serviceId, { GH_TOKEN: "test-only" });
		await provider.redeploy(created.serviceId);
		await provider.delete(created.serviceId, created.volumeId);
		expect(await provider.list()).toEqual([]);
		expect(await provider.getStatus(created.serviceId)).toBe("failed");
	});
});

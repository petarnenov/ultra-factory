import type { RailwayService, WorkerProvider, WorkerSpec, WorkerStatus } from "./railway-client.ts";

export class FakeRailwayProvider implements WorkerProvider {
	#service?: { id: string; name: string; volumeId: string; env: Record<string, string>; status: WorkerStatus };
	readonly #url: string;
	#nextId = 1;

	constructor(workerUrl: string) {
		this.#url = workerUrl;
	}

	async create(spec: WorkerSpec): Promise<{ serviceId: string; volumeId: string; environmentId: string; url: string }> {
		if (this.#service) throw new Error("Fake provider supports one worker at a time");
		const id = `fake-${this.#nextId++}`;
		const volumeId = `fake-vol-${this.#nextId - 1}`;
		this.#service = { id, name: spec.name, volumeId, env: { ...spec.env }, status: "running" };
		return { serviceId: id, volumeId, environmentId: "fake-env", url: this.#url };
	}

	async getStatus(serviceId: string): Promise<WorkerStatus> {
		return this.#service?.id === serviceId ? this.#service.status : "failed";
	}

	async list(): Promise<RailwayService[]> {
		return this.#service ? [{ id: this.#service.id, name: this.#service.name }] : [];
	}

	async setVariables(serviceId: string, variables: Record<string, string>): Promise<void> {
		if (this.#service?.id !== serviceId) throw new Error("Fake worker was not found");
		this.#service.env = { ...this.#service.env, ...variables };
	}

	async redeploy(serviceId: string): Promise<void> {
		if (this.#service?.id !== serviceId) throw new Error("Fake worker was not found");
		this.#service.status = "running";
	}

	async delete(serviceId: string, _volumeId: string): Promise<void> {
		if (this.#service?.id === serviceId) this.#service = undefined;
	}
}

export type WorkerSpec = {
	name: string;
	image: string;
	env: Record<string, string>;
	volumeMountPath: "/data";
	urlMode?: "public" | "private";
	region?: string;
};

export type WorkerStatus = "provisioning" | "running" | "failed" | "stopped";

export interface WorkerProvider {
	create(spec: WorkerSpec): Promise<{ serviceId: string; volumeId: string; environmentId: string; url: string }>;
	getStatus(serviceId: string): Promise<WorkerStatus>;
	setVariables(serviceId: string, variables: Record<string, string>): Promise<void>;
	redeploy(serviceId: string): Promise<void>;
	delete(serviceId: string, volumeId: string): Promise<void>;
}

export class RailwayAuthError extends Error {
	constructor() {
		super("Railway authentication failed or the token lacks permission for this operation");
		this.name = "RailwayAuthError";
	}
}

export class RailwayQuotaError extends Error {
	constructor() {
		super("Railway resource quota was exceeded");
		this.name = "RailwayQuotaError";
	}
}

export class RailwayNotFound extends Error {
	constructor() {
		super("Railway resource was not found");
		this.name = "RailwayNotFound";
	}
}

export class RailwayApiError extends Error {
	constructor() {
		super("Railway API request failed");
		this.name = "RailwayApiError";
	}
}

export const railwayQueries = {
	createService: `mutation($input: ServiceCreateInput!) { serviceCreate(input: $input) { id name } }`,
	createVolume: `mutation($input: VolumeCreateInput!) { volumeCreate(input: $input) { id name } }`,
	createDomain: `mutation($input: ServiceDomainCreateInput!) { serviceDomainCreate(input: $input) { id domain } }`,
	upsertVariable: `mutation($input: VariableUpsertInput!) { variableUpsert(input: $input) }`,
	status: `query($serviceId: String!, $environmentId: String!) { serviceInstance(serviceId: $serviceId, environmentId: $environmentId) { serviceName latestDeployment { id status createdAt } } }`,
	redeploy: `mutation($serviceId: String!, $environmentId: String!) { serviceInstanceRedeploy(serviceId: $serviceId, environmentId: $environmentId) }`,
	updateServiceInstance: `mutation($serviceId: String!, $environmentId: String!, $input: ServiceInstanceUpdateInput!) { serviceInstanceUpdate(serviceId: $serviceId, environmentId: $environmentId, input: $input) }`,
	deleteService: `mutation($id: String!) { serviceDelete(id: $id) }`,
	deleteVolume: `mutation($volumeId: String!) { volumeDelete(volumeId: $volumeId) }`,
} as const;

type GraphQLError = { message?: string; extensions?: { code?: string }; path?: Array<string | number> };
type GraphQLResponse = { data?: Record<string, unknown> | null; errors?: GraphQLError[] };

export type RailwayClientOptions = {
	token: string;
	tokenKind?: "project" | "account";
	projectId: string;
	environmentId: string;
	fetch?: typeof globalThis.fetch;
	retryDelayMs?: number;
	sleep?: (milliseconds: number) => Promise<void>;
};

const endpoint = "https://backboard.railway.com/graphql/v2";
const timeoutMs = 30_000;

export class RailwayClient implements WorkerProvider {
	private readonly fetchImpl: typeof globalThis.fetch;
	private readonly retryDelayMs: number;
	private readonly sleep: (milliseconds: number) => Promise<void>;
	private readonly authHeader: "Project-Access-Token" | "Authorization";
	private readonly options: RailwayClientOptions;

	constructor(options: RailwayClientOptions) {
		this.options = options;
		if (!options.token) throw new Error("Railway token is required");
		this.fetchImpl = options.fetch ?? globalThis.fetch;
		this.retryDelayMs = options.retryDelayMs ?? 250;
		this.sleep = options.sleep ?? ((milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)));
		this.authHeader = (options.tokenKind ?? "project") === "project" ? "Project-Access-Token" : "Authorization";
	}

	async create(spec: WorkerSpec): Promise<{ serviceId: string; volumeId: string; environmentId: string; url: string }> {
		const service = await this.request<{ serviceCreate: { id: string } }>(railwayQueries.createService, {
			input: {
				projectId: this.options.projectId,
				environmentId: this.options.environmentId,
				name: spec.name,
				source: { image: spec.image },
			},
		});
		const serviceId = service.serviceCreate?.id;
		if (!serviceId) throw new RailwayApiError();

		const volume = await this.request<{ volumeCreate: { id: string } }>(railwayQueries.createVolume, {
			input: {
				projectId: this.options.projectId,
				environmentId: this.options.environmentId,
				serviceId,
				mountPath: spec.volumeMountPath,
				...(spec.region ? { region: spec.region } : {}),
			},
		});
		const volumeId = volume.volumeCreate?.id;
		if (!volumeId) throw new RailwayApiError();
		let url: string;
		if (spec.urlMode === "private") {
			url = `http://${spec.name}.railway.internal:3011`;
		} else {
			const domain = await this.request<{ serviceDomainCreate: { domain: string } }>(railwayQueries.createDomain, {
				input: { serviceId, environmentId: this.options.environmentId, targetPort: 3011 },
			});
			if (!domain.serviceDomainCreate?.domain) throw new RailwayApiError();
			url = `https://${domain.serviceDomainCreate.domain}`;
		}
		if (spec.region) {
			await this.request(railwayQueries.updateServiceInstance, {
				serviceId,
				environmentId: this.options.environmentId,
				input: { region: spec.region },
			});
		}

		await this.setVariables(serviceId, spec.env);
		await this.redeploy(serviceId);
		return { serviceId, volumeId, environmentId: this.options.environmentId, url };
	}

	async setVariables(serviceId: string, variables: Record<string, string>): Promise<void> {
		for (const [name, value] of Object.entries(variables)) {
			await this.request(railwayQueries.upsertVariable, {
				input: { projectId: this.options.projectId, environmentId: this.options.environmentId, serviceId, name, value, skipDeploys: true },
			});
		}
	}

	async getStatus(serviceId: string): Promise<WorkerStatus> {
		const result = await this.request<{ serviceInstance: { latestDeployment?: { status?: string } | null } | null }>(railwayQueries.status, {
			serviceId,
			environmentId: this.options.environmentId,
		});
		const deployment = result.serviceInstance?.latestDeployment;
		if (!result.serviceInstance) throw new RailwayNotFound();
		if (!deployment) return "provisioning";
		switch (deployment.status?.toUpperCase()) {
			case "SUCCESS": return "running";
			case "FAILED":
			case "CRASHED": return "failed";
			case "REMOVED": return "stopped";
			case "SLEEPING":
			case "STOPPED": return "stopped";
			default: return "provisioning";
		}
	}

	async redeploy(serviceId: string): Promise<void> {
		await this.request(railwayQueries.redeploy, { serviceId, environmentId: this.options.environmentId });
	}

	async delete(serviceId: string, volumeId: string): Promise<void> {
		await this.request(railwayQueries.deleteService, { id: serviceId });
		await this.request(railwayQueries.deleteVolume, { volumeId });
	}

	private async request<T>(query: string, variables?: Record<string, unknown>): Promise<T> {
		for (let attempt = 0; attempt < 4; attempt += 1) {
			let response: Response;
			try {
				response = await this.fetchImpl(endpoint, {
					method: "POST",
					headers: {
						"Content-Type": "application/json",
						[this.authHeader]: this.authHeader === "Authorization" ? `Bearer ${this.options.token}` : this.options.token,
					},
					body: JSON.stringify({ query, variables }),
					signal: AbortSignal.timeout(timeoutMs),
				});
			} catch {
				throw new RailwayApiError();
			}
			if ((response.status === 429 || response.status >= 500) && attempt < 3) {
				const retryAfterHeader = response.status === 429 ? response.headers.get("Retry-After") : null;
				const retryAfterSeconds = retryAfterHeader === null ? Number.NaN : Number(retryAfterHeader);
				const retryAfterDate = retryAfterHeader === null ? Number.NaN : Date.parse(retryAfterHeader);
				const retryAfter = Number.isFinite(retryAfterSeconds)
					? retryAfterSeconds * 1000
					: Number.isFinite(retryAfterDate) ? Math.max(0, retryAfterDate - Date.now()) : 0;
				const delay = Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : this.retryDelayMs * 2 ** attempt;
				await this.sleep(delay);
				continue;
			}
			if (response.status === 401 || response.status === 403) throw new RailwayAuthError();
			if (response.status === 429) throw new RailwayQuotaError();
			if (!response.ok) throw new RailwayApiError();

			let body: GraphQLResponse;
			try {
				body = await response.json() as GraphQLResponse;
			} catch {
				throw new RailwayApiError();
			}
			if (body.errors?.length) this.throwGraphQLError(body.errors);
			if (body.data == null) throw new RailwayApiError();
			return body.data as T;
		}
		throw new RailwayApiError();
	}

	private throwGraphQLError(errors: GraphQLError[]): never {
		const message = errors.map((error) => error.message ?? "").join(" ").toLowerCase();
		const code = errors.map((error) => error.extensions?.code ?? "").join(" ").toLowerCase();
		if (message.includes("not authorized") || message.includes("unauthorized") || code.includes("unauthorized") || code.includes("forbidden")) {
			throw new RailwayAuthError();
		}
		if (message.includes("not found") || message.includes("does not exist")) throw new RailwayNotFound();
		if (message.includes("quota") || message.includes("limit exceeded") || code.includes("quota")) throw new RailwayQuotaError();
		throw new RailwayApiError();
	}
}

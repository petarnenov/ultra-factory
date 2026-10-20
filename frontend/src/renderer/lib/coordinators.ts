import type { components } from "../../api/schema";
import { apiErrorMessage } from "./api-client";
import { clientForSessionHost } from "./host-clients";

export type CoordinatorRoster = components["schemas"]["CoordinatorRosterResponse"];
export type CoordinatorRosterEntry = components["schemas"]["CoordinatorRosterEntry"];
export type CoordinatorsConfig = components["schemas"]["DomainCoordinatorsConfig"];
export type AgentProfile = components["schemas"]["AgentProfile"];
export type AgentSelection = components["schemas"]["AgentSelection"];
export type ProjectConfig = components["schemas"]["ProjectConfig"];

/** Hard ceilings a project may never exceed (ADR 0010 amendment). */
export const COORDINATOR_HARD_LIMITS = { coordinators: 4, workersPerCoordinator: 6, workers: 12 } as const;
/** Defaults used when a limit is blank or zero. */
export const COORDINATOR_DEFAULT_LIMITS = { coordinators: 2, workersPerCoordinator: 3, workers: 6 } as const;
export const COORDINATOR_SCOPE_MAX = 200;

export const coordinatorRosterQueryKey = (projectId: string, hostId?: string) =>
	["coordinator-roster", hostId ?? "", projectId] as const;

/** The daemon's derived ownership hierarchy for one project. */
export async function fetchCoordinatorRoster(projectId: string, hostId?: string): Promise<CoordinatorRoster> {
	const { data, error } = await clientForSessionHost(hostId).GET("/api/v1/projects/{id}/coordinators", {
		params: { path: { id: projectId } },
	});
	if (error || !data) throw new Error(apiErrorMessage(error, "Could not load coordinators"));
	return data;
}

export type SelectionMode = "inherit" | "fixed" | "random";

export type SelectionForm = {
	mode: SelectionMode;
	profileId: string;
	profileIds: string[];
};

export type ProfileForm = {
	id: string;
	label: string;
	harness: string;
	model: string;
	effort: string;
	weight: string;
};

/** The Coordinators settings section as typed by the operator. */
export type CoordinatorsForm = {
	enabled: boolean;
	maxCoordinators: string;
	maxWorkersPerCoordinator: string;
	maxWorkers: string;
	allowedHosts: string[];
	allowLocalWorkers: boolean;
	coordinatorAgent: SelectionForm;
	workerAgent: SelectionForm;
	rootWorkerAgent: SelectionForm;
	agentProfiles: ProfileForm[];
};

const selectionFormFrom = (sel: AgentSelection | undefined): SelectionForm => {
	const mode = sel?.mode === "fixed" || sel?.mode === "random" ? sel.mode : "inherit";
	return { mode, profileId: sel?.profileId ?? "", profileIds: sel?.profileIds ?? [] };
};

export function coordinatorsFormFrom(config: CoordinatorsConfig | undefined): CoordinatorsForm {
	const limit = (value: number | undefined) => (value ? String(value) : "");
	return {
		enabled: config?.enabled ?? false,
		maxCoordinators: limit(config?.maxCoordinators),
		maxWorkersPerCoordinator: limit(config?.maxWorkersPerCoordinator),
		maxWorkers: limit(config?.maxWorkers),
		allowedHosts: config?.allowedHosts ?? [],
		allowLocalWorkers: config?.allowLocalWorkers ?? false,
		coordinatorAgent: selectionFormFrom(config?.coordinatorAgent),
		workerAgent: selectionFormFrom(config?.workerAgent),
		rootWorkerAgent: selectionFormFrom(config?.rootWorkerAgent),
		agentProfiles: (config?.agentProfiles ?? []).map((profile) => ({
			id: profile.id,
			label: profile.label ?? "",
			harness: profile.harness,
			model: profile.model,
			effort: profile.effort,
			weight: profile.weight ? String(profile.weight) : "",
		})),
	};
}

export type CoordinatorsFormErrorKey =
	| "coordinators.errors.limitRange"
	| "coordinators.errors.workersPerCoordinatorAboveWorkers"
	| "coordinators.errors.profileIdRequired"
	| "coordinators.errors.profileIdDuplicate"
	| "coordinators.errors.profileHarnessRequired";

export type CoordinatorsFormErrors = {
	maxCoordinators?: CoordinatorsFormErrorKey;
	maxWorkersPerCoordinator?: CoordinatorsFormErrorKey;
	maxWorkers?: CoordinatorsFormErrorKey;
	profiles?: CoordinatorsFormErrorKey;
};

/** Parses a limit field: blank is the default, otherwise 0..hard maximum. */
export function parseLimit(value: string, hard: number): number | null | "invalid" {
	const trimmed = value.trim();
	if (trimmed === "") return null;
	if (!/^\d+$/.test(trimmed)) return "invalid";
	const parsed = Number(trimmed);
	return parsed <= hard ? parsed : "invalid";
}

/** A non-negative integer field, or undefined when blank or not a number. */
function optionalInt(value: string): number | undefined {
	const parsed = parseLimit(value, Number.MAX_SAFE_INTEGER);
	return typeof parsed === "number" ? parsed : undefined;
}

export function validateCoordinatorsForm(form: CoordinatorsForm): CoordinatorsFormErrors {
	const errors: CoordinatorsFormErrors = {};
	const coordinators = parseLimit(form.maxCoordinators, COORDINATOR_HARD_LIMITS.coordinators);
	const perCoordinator = parseLimit(form.maxWorkersPerCoordinator, COORDINATOR_HARD_LIMITS.workersPerCoordinator);
	const workers = parseLimit(form.maxWorkers, COORDINATOR_HARD_LIMITS.workers);
	if (coordinators === "invalid") errors.maxCoordinators = "coordinators.errors.limitRange";
	if (perCoordinator === "invalid") errors.maxWorkersPerCoordinator = "coordinators.errors.limitRange";
	if (workers === "invalid") errors.maxWorkers = "coordinators.errors.limitRange";
	const effectivePerCoordinator = perCoordinator === "invalid" ? null : perCoordinator ?? COORDINATOR_DEFAULT_LIMITS.workersPerCoordinator;
	const effectiveWorkers = workers === "invalid" ? null : workers ?? COORDINATOR_DEFAULT_LIMITS.workers;
	if (effectivePerCoordinator !== null && effectiveWorkers !== null && effectivePerCoordinator > effectiveWorkers) {
		errors.maxWorkersPerCoordinator = "coordinators.errors.workersPerCoordinatorAboveWorkers";
	}
	const seen = new Set<string>();
	for (const profile of form.agentProfiles) {
		const id = profile.id.trim();
		if (id === "") errors.profiles = "coordinators.errors.profileIdRequired";
		else if (seen.has(id)) errors.profiles = "coordinators.errors.profileIdDuplicate";
		else if (profile.harness.trim() === "") errors.profiles = "coordinators.errors.profileHarnessRequired";
		seen.add(id);
	}
	return errors;
}

const selectionConfig = (sel: SelectionForm): AgentSelection | undefined => {
	if (sel.mode === "fixed") return { mode: "fixed", profileId: sel.profileId };
	if (sel.mode === "random") return { mode: "random", profileIds: sel.profileIds };
	return undefined;
};

/** Builds the coordinators block; blank limits are omitted for defaults. */
export function coordinatorsConfigFrom(form: CoordinatorsForm): CoordinatorsConfig {
	return {
		enabled: form.enabled,
		maxCoordinators: optionalInt(form.maxCoordinators),
		maxWorkersPerCoordinator: optionalInt(form.maxWorkersPerCoordinator),
		maxWorkers: optionalInt(form.maxWorkers),
		allowedHosts: form.allowedHosts.length ? form.allowedHosts : undefined,
		allowLocalWorkers: form.allowLocalWorkers || undefined,
		coordinatorAgent: selectionConfig(form.coordinatorAgent),
		workerAgent: selectionConfig(form.workerAgent),
		rootWorkerAgent: selectionConfig(form.rootWorkerAgent),
		agentProfiles: form.agentProfiles.length
			? form.agentProfiles.map((profile) => ({
					id: profile.id.trim(),
					label: profile.label.trim() || undefined,
					harness: profile.harness.trim(),
					model: profile.model.trim(),
					effort: profile.effort.trim(),
					weight: optionalInt(profile.weight),
				}))
			: undefined,
	};
}

/** Replaces only the coordinators block; other config fields are kept. */
export function withCoordinatorsConfig(
	config: ProjectConfig,
	coordinators: CoordinatorsConfig,
): ProjectConfig {
	return { ...config, coordinators };
}

export type SpawnCoordinatorForm = {
	scope: string;
	workerLimit: string;
	hostSlots: { hostId: string; slots: string }[];
	coordinatorAgent: "inherit" | "profile" | "custom";
	coordinatorProfileId: string;
	customHarness: string;
	customModel: string;
	customEffort: string;
	workerAgent: SelectionForm;
	brief: string;
};

export type SpawnCoordinatorErrors = Partial<Record<"scope" | "workerLimit" | "hostSlots" | "brief" | "coordinatorAgent", CoordinatorsFormErrorKey | SpawnFormErrorKey>>;

export type SpawnFormErrorKey =
	| "coordinators.start.errors.scopeRequired"
	| "coordinators.start.errors.scopeTooLong"
	| "coordinators.start.errors.workerLimitRange"
	| "coordinators.start.errors.hostSlotsTotal"
	| "coordinators.start.errors.hostSlotInvalid"
	| "coordinators.start.errors.hostSlotDuplicate"
	| "coordinators.start.errors.hostNotAllowed"
	| "coordinators.start.errors.briefRequired"
	| "coordinators.start.errors.customAgentRequired";

/** Validates the start dialog against the project's effective limits. */
export function validateSpawnCoordinator(
	form: SpawnCoordinatorForm,
	limits: { workersPerCoordinator: number; allowedHosts: string[] },
): SpawnCoordinatorErrors {
	const errors: SpawnCoordinatorErrors = {};
	const scope = form.scope.trim();
	if (scope === "") errors.scope = "coordinators.start.errors.scopeRequired";
	else if (scope.length > COORDINATOR_SCOPE_MAX) errors.scope = "coordinators.start.errors.scopeTooLong";
	const workerLimit = form.workerLimit.trim() === "" ? limits.workersPerCoordinator : Number(form.workerLimit);
	if (!Number.isInteger(workerLimit) || workerLimit < 1 || workerLimit > limits.workersPerCoordinator) {
		errors.workerLimit = "coordinators.start.errors.workerLimitRange";
	}
	const seen = new Set<string>();
	let total = 0;
	for (const row of form.hostSlots) {
		const hostId = row.hostId.trim();
		if (hostId === "") continue;
		const slots = Number(row.slots);
		if (!Number.isInteger(slots) || slots < 1) {
			errors.hostSlots = "coordinators.start.errors.hostSlotInvalid";
			return errors;
		}
		if (seen.has(hostId)) {
			errors.hostSlots = "coordinators.start.errors.hostSlotDuplicate";
			return errors;
		}
		if (limits.allowedHosts.length > 0 && !limits.allowedHosts.includes(hostId)) {
			errors.hostSlots = "coordinators.start.errors.hostNotAllowed";
			return errors;
		}
		seen.add(hostId);
		total += slots;
	}
	if (!errors.workerLimit && total > workerLimit) errors.hostSlots = "coordinators.start.errors.hostSlotsTotal";
	if (form.brief.trim() === "") errors.brief = "coordinators.start.errors.briefRequired";
	if (form.coordinatorAgent === "custom" && form.customHarness.trim() === "") {
		errors.coordinatorAgent = "coordinators.start.errors.customAgentRequired";
	}
	return errors;
}

/** Maps a daemon refusal code to a readable key; unknown codes return null. */
export const COORDINATOR_ERROR_KEYS = {
	COORDINATOR_ROOT_REQUIRED: "coordinators.errors.rootRequired",
	COORDINATOR_LIMIT_REACHED: "coordinators.errors.limitReached",
	COORDINATOR_WORKER_LIMIT_INVALID: "coordinators.errors.workerLimitInvalid",
	COORDINATOR_WORKER_LIMIT_REACHED: "coordinators.errors.workerLimitReached",
	COORDINATOR_HOST_NOT_ALLOWED: "coordinators.errors.hostNotAllowed",
	COORDINATOR_HOST_SLOTS_FULL: "coordinators.errors.hostSlotsFull",
	COORDINATOR_LOCAL_WORKER_REFUSED: "coordinators.errors.localWorkerRefused",
	COORDINATOR_LIMITS_INVALID: "coordinators.errors.limitsInvalid",
	COORDINATOR_LIMITS_ORIGIN_INVALID: "coordinators.errors.operatorRefused",
	OPERATOR_MARKER_NOT_ALLOWED: "coordinators.errors.operatorRefused",
	COORDINATOR_PROFILE_NOT_ALLOWED: "coordinators.errors.profileNotAllowed",
	WORKER_PROFILE_INVALID: "coordinators.errors.profileInvalid",
	WORKER_PROFILE_UNAVAILABLE: "coordinators.errors.profileUnavailable",
	WORKER_PROFILE_NOT_ALLOWED: "coordinators.errors.profileNotAllowed",
	COORDINATOR_SCOPE_REQUIRED: "coordinators.start.errors.scopeRequired",
	COORDINATOR_SCOPE_TOO_LONG: "coordinators.start.errors.scopeTooLong",
	COORDINATOR_TERMINATED: "coordinators.errors.terminated",
	OWNER_TERMINATED: "coordinators.errors.terminated",
	OWNER_NOT_COORDINATOR: "coordinators.errors.notCoordinator",
	SESSION_NOT_FOUND: "coordinators.errors.notFound",
} as const;

export type CoordinatorErrorKey = (typeof COORDINATOR_ERROR_KEYS)[keyof typeof COORDINATOR_ERROR_KEYS];

/** The readable message key for a daemon error, or null for its own text. */
export function coordinatorErrorKey(error: unknown): CoordinatorErrorKey | null {
	if (typeof error !== "object" || error === null) return null;
	const code = (error as { code?: unknown }).code;
	if (typeof code !== "string") return null;
	return Object.hasOwn(COORDINATOR_ERROR_KEYS, code) ? COORDINATOR_ERROR_KEYS[code as keyof typeof COORDINATOR_ERROR_KEYS] : null;
}

/** Operator-initiated write body: the UI acts on behalf of the live root. */
const OPERATOR = { operatorInitiated: true, origin: "ui" } as const;

type SpawnHarness = components["schemas"]["SpawnSessionRequest"]["harness"];

export async function spawnCoordinatorOnBehalfOfRoot(
	projectId: string,
	form: SpawnCoordinatorForm,
	hostId?: string,
) {
	const slots = form.hostSlots
		.filter((row) => row.hostId.trim() !== "")
		.map((row) => ({ hostId: row.hostId.trim(), slots: Number(row.slots) }));
	const coordinatorAgent =
		form.coordinatorAgent === "profile"
			? { mode: "fixed" as const, profileId: form.coordinatorProfileId }
			: undefined;
	const custom = form.coordinatorAgent === "custom";
	const workerLimit = form.workerLimit.trim() === "" ? undefined : Number(form.workerLimit);
	const { data, error } = await clientForSessionHost(hostId).POST("/api/v1/sessions", {
		body: {
			projectId,
			kind: "coordinator",
			scope: form.scope.trim(),
			prompt: form.brief,
			workerLimit,
			hostSlots: slots.length ? slots : undefined,
			coordinatorAgent,
			harness: custom ? (form.customHarness.trim() as SpawnHarness) : undefined,
			model: custom ? form.customModel.trim() || undefined : undefined,
			effort: custom ? form.customEffort.trim() || undefined : undefined,
			workerAgent: selectionConfig(form.workerAgent),
			...OPERATOR,
		},
	});
	if (error || !data) throw error ?? new Error("Could not start the coordinator");
	return data.session;
}

export async function setCoordinatorLimitsOnBehalfOfRoot(
	coordinatorId: string,
	body: { workerLimit?: number; hostSlots?: { hostId: string; slots: number }[] },
	hostId?: string,
) {
	const { data, error } = await clientForSessionHost(hostId).PUT("/api/v1/sessions/{sessionId}/coordinator-limits", {
		params: { path: { sessionId: coordinatorId } },
		body: { ...body, ...OPERATOR },
	});
	if (error || !data) throw error ?? new Error("Could not change the coordinator limits");
	return data;
}

/** Reassigns a worker or a remote link; the daemon audits the owner change. */
export async function reassignSessionOwner(
	sessionId: string,
	ownerSessionId: string | null,
	requestedBySessionId: string,
	hostId?: string,
) {
	const { data, error } = await clientForSessionHost(hostId).PUT("/api/v1/sessions/{sessionId}/owner", {
		params: { path: { sessionId } },
		body: { ownerSessionId, requestedBySessionId },
	});
	if (error || !data) throw error ?? new Error("Could not reassign the session");
	return data;
}

export async function stopSession(sessionId: string, hostId?: string) {
	const { data, error } = await clientForSessionHost(hostId).POST("/api/v1/sessions/{sessionId}/kill", {
		params: { path: { sessionId } },
	});
	if (error || !data) throw error ?? new Error("Could not stop the session");
	return data;
}

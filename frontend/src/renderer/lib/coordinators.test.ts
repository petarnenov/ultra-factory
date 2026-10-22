import { describe, expect, test } from "vitest";
import {
	coordinatorErrorKey,
	type CoordinatorsConfig,
	coordinatorsConfigFrom,
	coordinatorsFormFrom,
	validateCoordinatorsForm,
	validateSpawnCoordinator,
	withCoordinatorsConfig,
	type CoordinatorsForm,
	type SpawnCoordinatorForm,
} from "./coordinators";

const baseForm = (overrides: Partial<CoordinatorsForm> = {}): CoordinatorsForm => ({
	enabled: true,
	maxCoordinators: "",
	maxWorkersPerCoordinator: "",
	maxWorkers: "",
	allowedHosts: [],
	allowLocalWorkers: false,
	coordinatorAgent: { mode: "inherit", profileId: "", profileIds: [] },
	workerAgent: { mode: "inherit", profileId: "", profileIds: [] },
	rootWorkerAgent: { mode: "inherit", profileId: "", profileIds: [] },
	agentProfiles: [],
	...overrides,
});

const spawnForm = (overrides: Partial<SpawnCoordinatorForm> = {}): SpawnCoordinatorForm => ({
	name: "Ada",
	scope: "docs",
	outOfScope: "billing",
	integrationBranch: "dev",
	branchPrefix: "co-docs",
	tasks: [{ taskId: "#1", title: "Docs", acceptance: "builds", files: "" }],
	briefEdited: true,
	briefPlaceholders: [],
	workerLimit: "",
	hostSlots: [],
	coordinatorAgent: "inherit",
	coordinatorProfileId: "",
	customHarness: "",
	customModel: "",
	customEffort: "",
	workerAgent: { mode: "inherit", profileId: "", profileIds: [] },
	brief: "Brief text",
	...overrides,
});

describe("withCoordinatorsConfig", () => {
	test("replaces only the coordinators block and keeps every other project field", () => {
		const config = {
			defaultBranch: "dev",
			autoReview: true,
			sessionPrefix: "ao",
			coordinators: { enabled: false, maxWorkers: 2 },
		};
		const next = withCoordinatorsConfig(config, { enabled: true, maxWorkers: 4 });
		expect(next).toEqual({
			defaultBranch: "dev",
			autoReview: true,
			sessionPrefix: "ao",
			coordinators: { enabled: true, maxWorkers: 4 },
		});
		expect(config.coordinators).toEqual({ enabled: false, maxWorkers: 2 });
	});
});

describe("coordinatorsConfigFrom", () => {
	test("omits blank limits so the daemon defaults apply", () => {
		const config = coordinatorsConfigFrom(baseForm({ maxCoordinators: "1" }));
		expect(config.maxCoordinators).toBe(1);
		expect(config.maxWorkers).toBeUndefined();
		expect(config.maxWorkersPerCoordinator).toBeUndefined();
	});

	test("round-trips a saved configuration through the form", () => {
		const saved = {
			enabled: true,
			maxCoordinators: 3,
			maxWorkersPerCoordinator: 2,
			maxWorkers: 8,
			allowedHosts: ["Linux"],
			allowLocalWorkers: true,
			coordinatorAgent: { mode: "fixed", profileId: "a" },
			agentProfiles: [{ id: "a", label: "Alpha", harness: "codex", model: "m", effort: "high", weight: 2 }],
		};
		const form = coordinatorsFormFrom(saved);
		expect(form.maxCoordinators).toBe("3");
		expect(coordinatorsConfigFrom(form)).toMatchObject(saved);
	});

	test("reads an empty or unknown mode as inherit (#502)", () => {
		const form = coordinatorsFormFrom({
			enabled: true,
			coordinatorAgent: { mode: "" },
			workerAgent: { mode: "shuffle" },
			rootWorkerAgent: {},
		});
		expect(form.coordinatorAgent.mode).toBe("inherit");
		expect(form.workerAgent.mode).toBe("inherit");
		expect(form.rootWorkerAgent.mode).toBe("inherit");
	});

	test("writes no empty mode back to the daemon (#502)", () => {
		const config = coordinatorsConfigFrom(
			coordinatorsFormFrom({
				enabled: true,
				coordinatorAgent: { mode: "" },
				workerAgent: { mode: "" },
				rootWorkerAgent: { mode: "" },
			}),
		);
		expect(JSON.stringify(config)).not.toContain('"mode":""');
		expect(config.coordinatorAgent).toBeUndefined();
		expect(config.workerAgent).toBeUndefined();
		expect(config.rootWorkerAgent).toBeUndefined();
	});
});

describe("validateCoordinatorsForm", () => {
	test("accepts blank limits and values up to the hard maximum", () => {
		expect(validateCoordinatorsForm(baseForm())).toEqual({});
		expect(validateCoordinatorsForm(baseForm({ maxCoordinators: "4", maxWorkersPerCoordinator: "6", maxWorkers: "12" }))).toEqual({});
	});

	test("rejects a limit above the hard maximum", () => {
		const errors = validateCoordinatorsForm(baseForm({ maxCoordinators: "5" }));
		expect(errors.maxCoordinators).toBe("coordinators.errors.limitRange");
	});

	test("rejects a per-coordinator limit above the project limit", () => {
		const errors = validateCoordinatorsForm(baseForm({ maxWorkersPerCoordinator: "4", maxWorkers: "3" }));
		expect(errors.maxWorkersPerCoordinator).toBe("coordinators.errors.workersPerCoordinatorAboveWorkers");
	});

	test("requires an ID and harness for every profile and unique IDs", () => {
		const profile = { id: "", label: "", harness: "codex", model: "", effort: "", weight: "" };
		expect(validateCoordinatorsForm(baseForm({ agentProfiles: [profile] })).profiles).toBe("coordinators.errors.profileIdRequired");
		const dup = { ...profile, id: "a", harness: "codex" };
		expect(validateCoordinatorsForm(baseForm({ agentProfiles: [dup, dup] })).profiles).toBe("coordinators.errors.profileIdDuplicate");
		expect(validateCoordinatorsForm(baseForm({ agentProfiles: [{ ...profile, id: "a", harness: "" }] })).profiles).toBe(
			"coordinators.errors.profileHarnessRequired",
		);
	});
});

describe("validateCoordinatorsForm selections", () => {
	const profile = { id: "fast", label: "", harness: "codex", model: "", effort: "", weight: "" };
	const sel = (mode: "inherit" | "fixed" | "random", profileId = "", profileIds: string[] = []) => ({ mode, profileId, profileIds });

	test("a fixed selection needs a profile that exists", () => {
		const form = baseForm({ agentProfiles: [profile], coordinatorAgent: sel("fixed") });
		expect(validateCoordinatorsForm(form).selections?.coordinatorAgent).toBe("coordinators.errors.selectionProfileRequired");
		const gone = baseForm({ agentProfiles: [profile], workerAgent: sel("fixed", "removed") });
		expect(validateCoordinatorsForm(gone).selections?.workerAgent).toBe("coordinators.errors.selectionProfileUnknown");
		const ok = baseForm({ agentProfiles: [profile], rootWorkerAgent: sel("fixed", "fast") });
		expect(validateCoordinatorsForm(ok).selections).toBeUndefined();
	});

	test("a random selection needs at least one profile, all of which exist", () => {
		expect(validateCoordinatorsForm(baseForm({ agentProfiles: [profile], workerAgent: sel("random") })).selections?.workerAgent).toBe(
			"coordinators.errors.selectionProfileRequired",
		);
		expect(
			validateCoordinatorsForm(baseForm({ agentProfiles: [profile], workerAgent: sel("random", "", ["fast", "old"]) })).selections
				?.workerAgent,
		).toBe("coordinators.errors.selectionProfileUnknown");
		expect(
			validateCoordinatorsForm(baseForm({ agentProfiles: [profile], workerAgent: sel("random", "", ["fast"]) })).selections,
		).toBeUndefined();
	});

	test("inherit needs no profile", () => {
		expect(validateCoordinatorsForm(baseForm()).selections).toBeUndefined();
	});
});

describe("validateSpawnCoordinator", () => {
	const limits = { workersPerCoordinator: 3, allowedHosts: [] as string[], profileIds: [] as string[] };

	test("refuses a brief that still has a template placeholder and names the first one", () => {
		const errors = validateSpawnCoordinator(spawnForm({ brief: "Assignment for <coordinator name>.\nScope: docs" }), limits);
		expect(errors.brief).toBe("coordinators.start.errors.briefPlaceholder");
		expect(errors.placeholder).toBe("<coordinator name>");
	});

	test("accepts a brief whose only angle brackets are coordinator instructions", () => {
		const brief = "Worker branch name: co-docs/<task-id>\nao coordinator report --ready-pr <number>@<head-sha>";
		expect(validateSpawnCoordinator(spawnForm({ brief }), limits).brief).toBeUndefined();
	});

	test("blocks a brief whose template placeholders are still unfilled", () => {
		const errors = validateSpawnCoordinator(spawnForm({ briefEdited: false, briefPlaceholders: ["<dev>"] }), limits);
		expect(errors.brief).toBe("coordinators.start.errors.briefPlaceholder");
		expect(errors.placeholder).toBe("<dev>");
	});

	test("requires a scope and a brief", () => {
		expect(validateSpawnCoordinator(spawnForm({ scope: "  " }), limits).scope).toBe("coordinators.start.errors.scopeRequired");
		expect(validateSpawnCoordinator(spawnForm({ brief: "" }), limits).brief).toBe("coordinators.start.errors.briefRequired");
	});

	test("bounds the scope at 200 characters", () => {
		expect(validateSpawnCoordinator(spawnForm({ scope: "x".repeat(201) }), limits).scope).toBe(
			"coordinators.start.errors.scopeTooLong",
		);
		expect(validateSpawnCoordinator(spawnForm({ scope: "x".repeat(200) }), limits).scope).toBeUndefined();
	});

	test("keeps the worker limit within the project's per-coordinator limit", () => {
		expect(validateSpawnCoordinator(spawnForm({ workerLimit: "2" }), limits).workerLimit).toBeUndefined();
		expect(validateSpawnCoordinator(spawnForm({ workerLimit: "4" }), limits).workerLimit).toBe(
			"coordinators.start.errors.workerLimitRange",
		);
	});

	test("accepts the spec example Linux:1, Z600:1 with workerLimit 2", () => {
		const errors = validateSpawnCoordinator(
			spawnForm({
				workerLimit: "2",
				hostSlots: [
					{ hostId: "Linux", slots: "1" },
					{ hostId: "Z600", slots: "1" },
				],
			}),
			{ workersPerCoordinator: 3, allowedHosts: [], profileIds: [] },
		);
		expect(errors).toEqual({});
	});

	test("refuses slots above the worker limit, duplicates and hosts outside the allowed list", () => {
		const over = spawnForm({
			workerLimit: "1",
			hostSlots: [
				{ hostId: "Linux", slots: "1" },
				{ hostId: "Z600", slots: "1" },
			],
		});
		expect(validateSpawnCoordinator(over, limits).hostSlots).toBe("coordinators.start.errors.hostSlotsTotal");
		const dup = spawnForm({
			hostSlots: [
				{ hostId: "Linux", slots: "1" },
				{ hostId: "Linux", slots: "1" },
			],
		});
		expect(validateSpawnCoordinator(dup, limits).hostSlots).toBe("coordinators.start.errors.hostSlotDuplicate");
		const notAllowed = spawnForm({ hostSlots: [{ hostId: "Mac", slots: "1" }] });
		expect(validateSpawnCoordinator(notAllowed, { workersPerCoordinator: 3, allowedHosts: ["Linux"], profileIds: [] }).hostSlots).toBe(
			"coordinators.start.errors.hostNotAllowed",
		);
	});

	test("requires a harness for a custom coordinator agent", () => {
		expect(validateSpawnCoordinator(spawnForm({ coordinatorAgent: "custom" }), limits).coordinatorAgent).toBe(
			"coordinators.start.errors.customAgentRequired",
		);
	});
});

describe("validateSpawnCoordinator agent selections", () => {
	const limits = { workersPerCoordinator: 3, allowedHosts: [] as string[], profileIds: ["fast", "deep"] };
	const sel = (mode: "inherit" | "fixed" | "random", profileId = "", profileIds: string[] = []) => ({ mode, profileId, profileIds });

	test("a coordinator profile must be picked and must still exist", () => {
		expect(validateSpawnCoordinator(spawnForm({ coordinatorAgent: "profile" }), limits).coordinatorAgent).toBe(
			"coordinators.errors.selectionProfileRequired",
		);
		expect(
			validateSpawnCoordinator(spawnForm({ coordinatorAgent: "profile", coordinatorProfileId: "removed" }), limits).coordinatorAgent,
		).toBe("coordinators.errors.selectionProfileUnknown");
		expect(
			validateSpawnCoordinator(spawnForm({ coordinatorAgent: "profile", coordinatorProfileId: "fast" }), limits).coordinatorAgent,
		).toBeUndefined();
	});

	test("a worker selection must name profiles that exist", () => {
		expect(validateSpawnCoordinator(spawnForm({ workerAgent: sel("fixed", "gone") }), limits).workerAgent).toBe(
			"coordinators.errors.selectionProfileUnknown",
		);
		expect(validateSpawnCoordinator(spawnForm({ workerAgent: sel("random") }), limits).workerAgent).toBe(
			"coordinators.errors.selectionProfileRequired",
		);
		expect(validateSpawnCoordinator(spawnForm({ workerAgent: sel("random", "", ["fast", "deep"]) }), limits).workerAgent).toBeUndefined();
	});

	test("inherit needs no profile for either agent", () => {
		expect(validateSpawnCoordinator(spawnForm(), { ...limits, profileIds: [] })).toEqual({});
	});
});

describe("coordinatorErrorKey", () => {
	test("maps a known daemon code to a readable message key", () => {
		expect(coordinatorErrorKey({ code: "COORDINATOR_HOST_SLOTS_FULL", message: "x" })).toBe("coordinators.errors.hostSlotsFull");
	});

	test("returns null for an unknown code so the daemon text shows", () => {
		expect(coordinatorErrorKey({ code: "SOMETHING_ELSE" })).toBeNull();
		expect(coordinatorErrorKey(new Error("boom"))).toBeNull();
	});
});

describe("coordinators block round trip", () => {
	// Every field the form edits, in the shape the daemon returns it.
	const loaded: CoordinatorsConfig = {
		enabled: true,
		maxCoordinators: 3,
		maxWorkersPerCoordinator: 3,
		maxWorkers: 9,
		allowedHosts: ["IMac", "Linux", "Z600"],
		allowLocalWorkers: true,
		agentProfiles: [
			{ id: "fast", label: "Fast", harness: "claude-code", model: "sonnet", effort: "low", weight: 2 },
			{ id: "deep", harness: "codex", model: "gpt-5", effort: "high" },
		],
		coordinatorAgent: { mode: "fixed", profileId: "deep" },
		workerAgent: { mode: "random", profileIds: ["fast", "deep"] },
		rootWorkerAgent: {},
	};

	test("a loaded block survives form and back unchanged", () => {
		const rebuilt = coordinatorsConfigFrom(coordinatorsFormFrom(loaded));
		expect(rebuilt).toEqual({ ...loaded, rootWorkerAgent: undefined });
		expect(JSON.parse(JSON.stringify(rebuilt))).toEqual(JSON.parse(JSON.stringify({ ...loaded, rootWorkerAgent: undefined })));
	});

	test("the body keeps only keys the daemon accepts", () => {
		const body = withCoordinatorsConfig({ defaultBranch: "dev", coordinators: loaded }, coordinatorsConfigFrom(coordinatorsFormFrom(loaded)));
		const allowed = new Set(["enabled", "maxCoordinators", "maxWorkersPerCoordinator", "maxWorkers", "agentProfiles", "coordinatorAgent", "workerAgent", "rootWorkerAgent", "allowedHosts", "allowLocalWorkers"]);
		for (const key of Object.keys(JSON.parse(JSON.stringify(body.coordinators)))) expect(allowed.has(key)).toBe(true);
	});
});

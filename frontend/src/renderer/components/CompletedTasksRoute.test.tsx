import { describe, expect, it } from "vitest";
import { buildCompletedTaskRefs } from "./CompletedTasksRoute";
import { LOCAL_HOST } from "../lib/hosts";

describe("buildCompletedTaskRefs", () => {
	it("always includes the selected project before explicit associations", () => {
		expect(buildCompletedTaskRefs(LOCAL_HOST, "proj-a", [{ hostId: "host-b", projectId: "proj-b" }])).toEqual([
			{ hostId: "local", projectId: "proj-a" },
			{ hostId: "host-b", projectId: "proj-b" },
		]);
	});

	it("does not infer or duplicate a project association by display name", () => {
		expect(buildCompletedTaskRefs(LOCAL_HOST, "proj-a", [
			{ hostId: "host-b", projectId: "proj-b" },
			{ hostId: "host-b", projectId: "proj-b" },
			{ hostId: "local", projectId: "proj-a" },
		])).toEqual([
			{ hostId: "local", projectId: "proj-a" },
			{ hostId: "host-b", projectId: "proj-b" },
		]);
	});

	it("uses an empty association list for projects without associations", () => {
		expect(buildCompletedTaskRefs("host-a", "proj-a", undefined)).toEqual([
			{ hostId: "host-a", projectId: "proj-a" },
		]);
	});
});

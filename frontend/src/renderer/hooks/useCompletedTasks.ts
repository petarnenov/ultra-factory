import { keepPreviousData, useQueries, useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiErrorMessage } from "../lib/api-client";
import { clientForSessionHost } from "../lib/host-clients";
import type { HostId } from "../lib/hosts";

export type CompletedTask = components["schemas"]["CompletedTaskSummary"];
export type CompletedTaskReport = components["schemas"]["CompletedTaskReportResponse"];
export type CompletedTaskOutcome = CompletedTask["outcome"];

/** A project on one host. Host and project IDs together are the identity. */
export type CompletedTaskProjectRef = { hostId: HostId; projectId: string };

export type HostedCompletedTask = CompletedTask & { ref: CompletedTaskProjectRef };

export type CompletedTaskSource = {
	ref: CompletedTaskProjectRef;
	status: "loading" | "error" | "ready";
	error?: string;
};

const PAGE_SIZE = 50;

async function fetchCompletedTasks(ref: CompletedTaskProjectRef, outcome: CompletedTaskOutcome | undefined, pages: number) {
	const client = clientForSessionHost(ref.hostId);
	const tasks: CompletedTask[] = [];
	let cursor: string | undefined;
	let loaded = 0;
	do {
		const { data, error } = await client.GET("/api/v1/projects/{projectId}/completed-tasks", {
			params: { path: { projectId: ref.projectId }, query: { outcome, limit: PAGE_SIZE, cursor } },
		});
		if (error) throw new Error(apiErrorMessage(error));
		tasks.push(...(data?.tasks ?? []));
		cursor = data?.nextCursor || undefined;
		loaded += 1;
	} while (cursor && loaded < pages);
	return { tasks, hasMore: Boolean(cursor) };
}

/** Newest recorded completion first; tasks without one follow. */
export function compareCompletedTasks(a: CompletedTask, b: CompletedTask): number {
	if (Boolean(a.completedAt) !== Boolean(b.completedAt)) return a.completedAt ? -1 : 1;
	if (a.completedAt && b.completedAt && a.completedAt !== b.completedAt) {
		return Date.parse(b.completedAt) - Date.parse(a.completedAt);
	}
	return Date.parse(b.createdAt) - Date.parse(a.createdAt);
}

// Reads every associated project separately, so one unreachable host
// leaves the others readable.
export function useCompletedTasks(refs: CompletedTaskProjectRef[], outcome: CompletedTaskOutcome | undefined, pages: number) {
	const results = useQueries({
		queries: refs.map((ref) => ({
			queryKey: ["completed-tasks", ref.hostId, ref.projectId, outcome ?? "all", pages],
			queryFn: () => fetchCompletedTasks(ref, outcome, pages),
			placeholderData: keepPreviousData,
			refetchInterval: 30_000,
		})),
	});
	const tasks: HostedCompletedTask[] = [];
	const sources: CompletedTaskSource[] = refs.map((ref, index) => {
		const result = results[index];
		for (const task of result?.data?.tasks ?? []) tasks.push({ ...task, ref });
		if (result?.data) return { ref, status: "ready" };
		if (result?.error) return { ref, status: "error", error: result.error.message };
		return { ref, status: "loading" };
	});
	tasks.sort(compareCompletedTasks);
	return {
		tasks,
		sources,
		hasMore: results.some((result) => result.data?.hasMore),
		isFetching: results.some((result) => result.isFetching),
	};
}

export function useCompletedTaskReport(ref: CompletedTaskProjectRef | null, sessionId: string | null) {
	return useQuery({
		queryKey: ["completed-task", ref?.hostId, ref?.projectId, sessionId],
		enabled: Boolean(ref && sessionId),
		queryFn: async () => {
			if (!ref || !sessionId) throw new Error("No task selected");
			const { data, error } = await clientForSessionHost(ref.hostId).GET("/api/v1/projects/{projectId}/completed-tasks/{sessionId}", {
				params: { path: { projectId: ref.projectId, sessionId } },
			});
			if (error) throw new Error(apiErrorMessage(error));
			if (!data) throw new Error("Completed task response was empty");
			return data;
		},
	});
}

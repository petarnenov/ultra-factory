import { useNavigate } from "@tanstack/react-router";
import { useMemo } from "react";
import { CompletedTasksView, type CompletedTaskSelection } from "./CompletedTasksView";
import type { CompletedTaskProjectRef } from "../hooks/useCompletedTasks";
import { LOCAL_HOST, type HostId } from "../lib/hosts";

export type CompletedTasksSearch = { task?: string };

export function validateCompletedTasksSearch(search: Record<string, unknown>): CompletedTasksSearch {
	return typeof search.task === "string" && search.task ? { task: search.task } : {};
}

/**
 * The project's own host is its only member today. Further host-qualified
 * members join `refs` once project association is configured explicitly.
 */
export function CompletedTasksRoute({ hostId = LOCAL_HOST, projectId, task }: { hostId?: HostId; projectId: string; task?: string }) {
	const navigate = useNavigate();
	const refs = useMemo<CompletedTaskProjectRef[]>(() => [{ hostId, projectId }], [hostId, projectId]);
	const selected: CompletedTaskSelection | null = task ? { ref: refs[0], sessionId: task } : null;
	return (
		<CompletedTasksView
			refs={refs}
			selected={selected}
			onSelect={(selection) => void navigate({ to: ".", search: selection ? { task: selection.sessionId } : {} })}
		/>
	);
}

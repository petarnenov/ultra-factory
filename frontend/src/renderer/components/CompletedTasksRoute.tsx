import { useNavigate } from "@tanstack/react-router";
import { useMemo } from "react";
import { CompletedTasksView, type CompletedTaskSelection } from "./CompletedTasksView";
import type { CompletedTaskProjectRef } from "../hooks/useCompletedTasks";
import { LOCAL_HOST, type HostId } from "../lib/hosts";

export type CompletedTasksSearch = { task?: string };

export function validateCompletedTasksSearch(search: Record<string, unknown>): CompletedTasksSearch {
	return typeof search.task === "string" && search.task ? { task: search.task } : {};
}

// The project's own host is its only member until host-qualified project
// association (#194) is configured; those members then join `refs`.
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

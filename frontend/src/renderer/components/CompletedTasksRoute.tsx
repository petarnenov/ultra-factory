import { useNavigate } from "@tanstack/react-router";
import { useMemo } from "react";
import { CompletedTasksView, type CompletedTaskSelection } from "./CompletedTasksView";
import type { CompletedTaskProjectRef } from "../hooks/useCompletedTasks";
import { useCompletedTasks } from "../hooks/useCompletedTasks";
import { useProjectIdentity } from "../hooks/useWorkspaceQuery";
import { LOCAL_HOST, refKey, type HostId } from "../lib/hosts";

export type CompletedTasksSearch = { task?: string };

export function validateCompletedTasksSearch(search: Record<string, unknown>): CompletedTasksSearch {
	return typeof search.task === "string" && search.task ? { task: search.task } : {};
}

// Host-qualified project references are owned by the project identity returned
// by the answering daemon (#194). Completed tasks use those explicit
// host/project pairs and never pair projects by repository URL or display name.
export function buildCompletedTaskRefs(
	hostId: HostId,
	projectId: string,
	aggregateProjects: Array<{ hostId: string; projectId: string }> | undefined,
): CompletedTaskProjectRef[] {
	const refs: CompletedTaskProjectRef[] = [{ hostId, projectId }];
	const seen = new Set([refKey({ host: hostId, id: projectId })]);
	for (const associated of aggregateProjects ?? []) {
		const key = refKey({ host: associated.hostId, id: associated.projectId });
		if (seen.has(key)) continue;
		seen.add(key);
		refs.push({ hostId: associated.hostId, projectId: associated.projectId });
	}
	return refs;
}

export function CompletedTasksRoute({ hostId = LOCAL_HOST, projectId, task }: { hostId?: HostId; projectId: string; task?: string }) {
	const navigate = useNavigate();
	const identity = useProjectIdentity(hostId === LOCAL_HOST ? undefined : hostId, projectId);
	const refs = useMemo(
		() => buildCompletedTaskRefs(hostId, projectId, identity.data?.aggregateProjects),
		[hostId, projectId, identity.data?.aggregateProjects],
	);
	const loaded = useCompletedTasks(refs, undefined, 1);
	const selectedRef =
		task !== undefined ? (loaded.tasks.find((entry) => entry.sessionId === task)?.ref ?? refs[0]) : null;
	const selected: CompletedTaskSelection | null =
		task !== undefined && selectedRef !== null ? { ref: selectedRef, sessionId: task } : null;

	return (
		<CompletedTasksView
			refs={refs}
			selected={selected}
			onSelect={(selection) => void navigate({ to: ".", search: selection ? { task: selection.sessionId } : {} })}
		/>
	);
}

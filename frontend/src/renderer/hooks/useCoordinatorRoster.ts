import { useQuery } from "@tanstack/react-query";
import { coordinatorRosterQueryKey, fetchCoordinatorRoster } from "../lib/coordinators";

/** Polls like the cloud worker list so ownership tracks the session board. */
export function useCoordinatorRoster(projectId: string, hostId?: string, enabled = true) {
	return useQuery({
		queryKey: coordinatorRosterQueryKey(projectId, hostId),
		queryFn: () => fetchCoordinatorRoster(projectId, hostId),
		enabled: enabled && Boolean(projectId),
		refetchInterval: 5000,
		retry: 1,
	});
}

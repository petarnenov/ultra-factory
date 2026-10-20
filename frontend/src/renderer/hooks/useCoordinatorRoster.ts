import { useQuery } from "@tanstack/react-query";
import { coordinatorRosterQueryKey, fetchCoordinatorRoster } from "../lib/coordinators";

/** Ownership changes rarely, so poll slowly and only while the window is visible. */
export function useCoordinatorRoster(projectId: string, hostId?: string, enabled = true) {
	return useQuery({
		queryKey: coordinatorRosterQueryKey(projectId, hostId),
		queryFn: () => fetchCoordinatorRoster(projectId, hostId),
		enabled: enabled && Boolean(projectId),
		refetchInterval: 15_000,
		refetchIntervalInBackground: false,
		retry: 1,
	});
}

import { useQuery } from "@tanstack/react-query";
import { coordinatorRosterQueryKey, fetchCoordinatorRoster } from "../lib/coordinators";

/** Events invalidate the roster; the 60 s poll is a visible-only fallback. */
export function useCoordinatorRoster(projectId: string, hostId?: string, enabled = true) {
	return useQuery({
		queryKey: coordinatorRosterQueryKey(projectId, hostId),
		queryFn: () => fetchCoordinatorRoster(projectId, hostId),
		enabled: enabled && Boolean(projectId),
		refetchInterval: 60_000,
		refetchIntervalInBackground: false,
		retry: 1,
	});
}

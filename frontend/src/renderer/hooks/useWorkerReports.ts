import { useQuery } from "@tanstack/react-query";
import { clientForSessionHost } from "../lib/host-clients";

export const workerReportsQueryKey = (projectId?: string, hostId?: string) => ["worker-reports", hostId ?? "local", projectId] as const;

export function useWorkerReports(projectId: string, hostId?: string, enabled = true) {
	return useQuery({
		queryKey: workerReportsQueryKey(projectId, hostId),
		enabled: Boolean(projectId) && enabled,
		queryFn: async () => {
			const { data, error } = await clientForSessionHost(hostId).GET("/api/v1/reports", { params: { query: { projectId } } });
			if (error) throw error;
			return data?.reports ?? [];
		},
		refetchInterval: 5_000,
	});
}

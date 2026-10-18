import { useInfiniteQuery } from "@tanstack/react-query";
import { clientForSessionHost } from "../lib/host-clients";

export const workerReportsQueryKey = (projectId?: string, hostId?: string) => ["worker-reports", hostId ?? "local", projectId] as const;

export const WORKER_REPORTS_PAGE_SIZE = 20;

export function useWorkerReports(projectId: string, hostId?: string, enabled = true) {
	const query = useInfiniteQuery({
		queryKey: workerReportsQueryKey(projectId, hostId),
		enabled: Boolean(projectId) && enabled,
		initialPageParam: "",
		queryFn: async ({ pageParam, signal }) => {
			const { data, error } = await clientForSessionHost(hostId).GET("/api/v1/reports", {
				signal,
				params: { query: { projectId, limit: WORKER_REPORTS_PAGE_SIZE, cursor: pageParam || undefined } },
			});
			if (error) throw error;
			return {
				reports: data?.reports ?? [],
				nextCursor: data?.nextCursor ?? "",
			};
		},
		getNextPageParam: (lastPage) => lastPage.nextCursor || undefined,
		refetchInterval: 5_000,
	});
	return {
		...query,
		data: deduplicateReports(query.data?.pages.flatMap((page) => page.reports ?? []) ?? []),
	};
}

function deduplicateReports<T extends { hostId?: string; sessionId: string; id: string }>(reports: T[]): T[] {
	const seen = new Set<string>();
	return reports.filter((report) => {
		const key = `${report.hostId || "local"}/${report.sessionId}/${report.id}`;
		if (seen.has(key)) return false;
		seen.add(key);
		return true;
	});
}

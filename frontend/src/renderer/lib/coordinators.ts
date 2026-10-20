import type { components } from "../../api/schema";
import { apiErrorMessage } from "./api-client";
import { clientForSessionHost } from "./host-clients";

export type CoordinatorRoster = components["schemas"]["CoordinatorRosterResponse"];
export type CoordinatorRosterEntry = components["schemas"]["CoordinatorRosterEntry"];

export const coordinatorRosterQueryKey = (projectId: string, hostId?: string) =>
	["coordinator-roster", hostId ?? "", projectId] as const;

/** The daemon's derived ownership hierarchy for one project. */
export async function fetchCoordinatorRoster(projectId: string, hostId?: string): Promise<CoordinatorRoster> {
	const { data, error } = await clientForSessionHost(hostId).GET("/api/v1/projects/{id}/coordinators", {
		params: { path: { id: projectId } },
	});
	if (error || !data) throw new Error(apiErrorMessage(error, "Could not load coordinators"));
	return data;
}

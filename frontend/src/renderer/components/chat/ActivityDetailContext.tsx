import { createContext, useContext } from "react";
import type { ConversationActivity } from "../../types/conversation";

/** Loads one activity whole; null where no daemon backs the surface. */
export type ActivityDetailLoader = (activityId: string) => Promise<ConversationActivity>;

const ActivityDetailContext = createContext<ActivityDetailLoader | null>(null);

export const ActivityDetailProvider = ActivityDetailContext.Provider;

export function useActivityDetailLoader(): ActivityDetailLoader | null {
	return useContext(ActivityDetailContext);
}

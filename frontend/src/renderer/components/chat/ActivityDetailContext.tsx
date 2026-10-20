import { createContext, useContext } from "react";
import type { ConversationActivity } from "../../types/conversation";

/**
 * Loads one activity whole. A conversation page bounds a file change's detail to a
 * compact diff; the card that is expanded asks for the rest through this. Null where
 * no daemon backs the surface (previews, fixtures), so the bounded copy is all there is.
 */
export type ActivityDetailLoader = (activityId: string) => Promise<ConversationActivity>;

const ActivityDetailContext = createContext<ActivityDetailLoader | null>(null);

export const ActivityDetailProvider = ActivityDetailContext.Provider;

export function useActivityDetailLoader(): ActivityDetailLoader | null {
	return useContext(ActivityDetailContext);
}

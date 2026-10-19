import type { SessionKind } from "../types/workspace";

/** A session role plus the reviewer surface a chat can be showing. */
export type ChatRole = SessionKind | "reviewer";

/** Maps a chat role to the established label used elsewhere in the product. */
export function chatRoleLabelKey(role: ChatRole) {
	if (role === "orchestrator") return "shell.orchestrator" as const;
	if (role === "reviewer") return "terminal.reviewer" as const;
	return "settings.models.workerRole" as const;
}

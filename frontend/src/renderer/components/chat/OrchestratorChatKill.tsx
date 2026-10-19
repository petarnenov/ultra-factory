import { useQueryClient } from "@tanstack/react-query";
import { Square } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	clearTerminateSessionState,
	useTerminateSession,
	useTerminateSessionState,
} from "../../hooks/useTerminateSession";
import type { WorkspaceSession } from "../../types/workspace";
import { SessionArchiveDialog, type SessionArchiveChoice } from "../SessionArchiveDialog";
import { TopbarActionError, TopbarButton } from "../TopbarButton";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";

/** Terminates the orchestrator from its own chat header.
 * It reuses the inspector's confirmation and kill mutation. */
export function OrchestratorChatKill({ hostId, session }: { hostId?: string; session: WorkspaceSession }) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const [confirmOpen, setConfirmOpen] = useState(false);
	const kill = useTerminateSession();
	const { error, isPending } = useTerminateSessionState(session.id, hostId);
	const ownedSession = hostId ? { ...session, hostId } : session;

	const confirmKill = ({ deleteArtifacts }: SessionArchiveChoice) => {
		setConfirmOpen(false);
		// Stay on the terminated chat so the terminal state remains visible.
		kill.mutate({ ...ownedSession, deleteArtifacts });
	};

	return (
		<div className="inline-flex items-center gap-1.5">
			<Tooltip>
				<TooltipTrigger asChild>
					<span className="inline-flex">
						<SessionArchiveDialog
							onConfirm={confirmKill}
							onOpenChange={setConfirmOpen}
							open={confirmOpen}
							session={ownedSession}
							trigger={
								<TopbarButton
									aria-label={t("chat.killOrchestrator.aria")}
									disabled={isPending}
									onClick={() => {
										clearTerminateSessionState(queryClient, session.id, hostId);
										setConfirmOpen(true);
									}}
									variant="kill"
								>
									<Square aria-hidden="true" className="size-3 fill-current" />
									{t("chat.killOrchestrator.action")}
								</TopbarButton>
							}
						/>
					</span>
				</TooltipTrigger>
				<TooltipContent side="bottom">
					{isPending ? t("chat.killOrchestrator.killing") : t("chat.killOrchestrator.aria")}
				</TooltipContent>
			</Tooltip>
			{error ? (
				<TopbarActionError className="max-w-48 truncate" title={error}>
					{error}
				</TopbarActionError>
			) : null}
		</div>
	);
}

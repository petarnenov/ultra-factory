import { createFileRoute } from "@tanstack/react-router";
import { AggregateProjectBoard } from "../components/AggregateProjectBoard";

export const Route = createFileRoute("/_shell/projects/$projectId_/aggregate")({
	component: AggregateProjectBoardRoute,
});

function AggregateProjectBoardRoute() {
	const { projectId } = Route.useParams();
	return <AggregateProjectBoard projectId={projectId} />;
}

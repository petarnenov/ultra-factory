import { createFileRoute } from "@tanstack/react-router";
import { CompletedTasksRoute, validateCompletedTasksSearch } from "../components/CompletedTasksRoute";
import { refKey } from "../lib/hosts";

export const Route = createFileRoute("/_shell/host/$hostId/project/$projectId_/completed")({
	validateSearch: validateCompletedTasksSearch,
	component: HostCompletedTasksRoute,
});

function HostCompletedTasksRoute() {
	const { hostId, projectId } = Route.useParams();
	const { task } = Route.useSearch();
	return <CompletedTasksRoute key={refKey({ host: hostId, id: projectId })} hostId={hostId} projectId={projectId} task={task} />;
}

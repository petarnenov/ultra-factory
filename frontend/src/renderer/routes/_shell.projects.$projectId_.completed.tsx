import { createFileRoute } from "@tanstack/react-router";
import { CompletedTasksRoute, validateCompletedTasksSearch } from "../components/CompletedTasksRoute";

export const Route = createFileRoute("/_shell/projects/$projectId_/completed")({
	validateSearch: validateCompletedTasksSearch,
	component: LocalCompletedTasksRoute,
});

function LocalCompletedTasksRoute() {
	const { projectId } = Route.useParams();
	const { task } = Route.useSearch();
	return <CompletedTasksRoute key={projectId} projectId={projectId} task={task} />;
}

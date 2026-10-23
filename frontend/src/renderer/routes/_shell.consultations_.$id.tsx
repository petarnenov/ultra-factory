import { createFileRoute } from "@tanstack/react-router";
import { ConsultationsView } from "../components/ConsultationsView";

export const Route = createFileRoute("/_shell/consultations_/$id")({ component: Consultation });
function Consultation() {
 const { id } = Route.useParams();
 return <ConsultationsView id={id} />;
}

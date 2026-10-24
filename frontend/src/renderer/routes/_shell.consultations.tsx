import { createFileRoute } from "@tanstack/react-router";
import { ConsultationsView } from "../components/ConsultationsView";

export const Route = createFileRoute("/_shell/consultations")({ component: ConsultationsView });

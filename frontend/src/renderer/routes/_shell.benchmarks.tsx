import { createFileRoute } from "@tanstack/react-router";
import { BenchmarksView } from "../components/benchmarks/BenchmarksView";

export const Route = createFileRoute("/_shell/benchmarks")({ component: BenchmarksView });

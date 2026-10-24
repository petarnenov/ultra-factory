import React from "react";
import { appI18n } from "../../../src/renderer/i18n";
import { I18nextProvider } from "react-i18next";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRootRoute, createRoute, createRouter, createHashHistory, Outlet, RouterProvider, Navigate } from "@tanstack/react-router";
import { ConsultationsView } from "../../../src/renderer/components/ConsultationsView";
import { setApiBaseUrl, setApiDaemonStatus } from "../../../src/renderer/lib/api-client";
import "../../../src/renderer/styles.css";

const mode = new URLSearchParams(location.search).get("state") || "records";
const id = (name) => btoa(`fixture/2026-10-23/${name}`).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
const summary = (name, partial) => ({
 id: id(name), name, date: "2026-10-23", host: "fixture-host", recordedHost: "iMac",
 requester: partial ? "" : "Recorded engineer", case: partial ? "Consultation with missing evidence" : "A recorded decision with a long case name that remains readable in a narrow panel",
 phase: partial ? "fallback_arbitration_complete" : "complete", decision: partial ? "approved_with_amendments" : "resolved",
 archive: "verified", delivery: partial ? "unconfirmed" : "delivered", closure: partial ? "" : "closed",
 mode: partial ? "fallback_arbitration" : "council", rounds: partial ? 0 : 1, proposalVersions: 1, phaseRounds: partial ? 1 : 3,
 syncedAt: "2026-10-23T21:30:00Z", partial,
 consulClosure: partial ? [{ consul: "deepseek", status: "not_started" }] : [],
 arbitration: partial ? { status: "completed", model: "claude-fable-5-1", effort: "medium" } : {},
});
const artifact = (name, phase, content, extra = {}) => ({ name, phase, content, hash: "a".repeat(64), ballots: [], missing: false, ...extra });
const records = [summary("complete", false), summary("partial", true)];
const evidence = (item) => {
 const artifacts = [artifact("request.md", "request", "## Recorded question\nHow should consultation evidence be indexed and displayed?\n\n[Blocked local link](file:///private/example.md)\n\n<script>alert('untrusted')</script>")];
 if (!item.partial) {
  for (const phase of ["independent", "critique"]) for (const consul of ["deepseek", "astra", "fable"]) artifacts.push(artifact(`${phase}/${consul}.md`, phase, `Original ${phase} evidence from **${consul}**.`, { consul }));
 }
 artifacts.push(artifact("proposal-1.md", "proposal", "## D639-1\nKeep the local archive canonical.", { round: 1 }));
 if (item.partial) artifacts.push(artifact("fallback-arbitration/fable.md", "arbitration", "## Original arbiter ruling\nThis is fallback arbitration. No council votes were obtained.", { consul: "fable" }));
 else for (const consul of ["deepseek", "astra", "fable"]) artifacts.push(artifact(`ballots/round-1/${consul}.md`, "ballot", "Proposal: proposal-1.md\nD639-1: APPROVE", { consul, round: 1, parsed: true, ballots: [{ decision: "D639-1", stance: "APPROVE" }] }));
 artifacts.push(artifact("final.md", "final", item.partial ? "Recorded fallback response. Delivery remains unconfirmed." : "## Recorded final answer\nKeep the archive canonical and use the graph as a read-only index.\n\n- Preserve original evidence.\n- Distinguish missing reports and unknown ballots."));
 if (item.partial) artifacts.push(artifact("astra.md", "collected", "", { consul: "astra", missing: true }));
 return { ...item, artifacts, warnings: item.partial ? ["Independent council reports are unavailable."] : [] };
};
const originalFetch = window.fetch.bind(window);
window.fetch = async (input, init) => {
 const request = new Request(input, init);
 const url = new URL(request.url);
 if (!url.pathname.startsWith("/api/v1/knowledge/consultations")) return originalFetch(input, init);
 await new Promise((resolve) => setTimeout(resolve, 200));
 if (mode === "unavailable") return Response.json({ code: "KNOWLEDGE_GRAPH_UNAVAILABLE" }, { status: 503 });
 if (mode === "unconfigured") return Response.json({ code: "NOT_IMPLEMENTED" }, { status: 501 });
 const key = url.pathname.split("/")[5];
 if (key) {
  const record = records.find((item) => item.id === key);
  return record ? Response.json(evidence(record)) : Response.json({}, { status: 404 });
 }
 return Response.json({ items: mode === "empty" ? [] : records, nextCursor: "", scan: { enabled: true, lastScan: "2026-10-23T21:30:00Z", imported: 2, examined: 2, durationMs: 15, error: "" } });
};
setApiBaseUrl(location.origin);
setApiDaemonStatus({ state: "ready" });
document.documentElement.dataset.theme = new URLSearchParams(location.search).get("theme") === "light" ? "light" : "dark";
const queryClient = new QueryClient();
const root = createRootRoute({ component: () => <div className="flex h-screen flex-col bg-background text-foreground"><nav className="flex flex-wrap gap-4 border-b border-border px-5 py-2 text-caption text-muted-foreground" aria-label="Review fixture states"><span>Review fixture</span>{["records", "empty", "unavailable", "unconfigured"].map((state) => <a key={state} href={`?state=${state}#/consultations`}>{state}</a>)}<a href={`?state=${mode}&theme=light#/consultations`}>Light theme</a></nav><Outlet /></div> });
const list = createRoute({ getParentRoute: () => root, path: "/consultations", component: ConsultationsView });
const detail = createRoute({ getParentRoute: () => root, path: "/consultations/$id", component: () => <ConsultationsView id={detail.useParams().id} /> });
const index = createRoute({ getParentRoute: () => root, path: "/", component: () => <Navigate to="/consultations" /> });
const router = createRouter({ history: createHashHistory(), routeTree: root.addChildren([list, detail, index]) });
createRoot(document.getElementById("root")).render(<I18nextProvider i18n={appI18n}><QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider></I18nextProvider>);

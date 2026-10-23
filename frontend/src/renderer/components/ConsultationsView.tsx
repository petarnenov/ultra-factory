import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient } from "../lib/api-client";
import { Button } from "./ui/button";
import { ChatMarkdown } from "./chat/ChatMarkdown";

type Detail = components["schemas"]["ConsultationDetail"];
type Summary = components["schemas"]["ConsultationSummary"];

const phaseLabels: Record<string, string> = { request: "Request", independent: "Independent analysis", critique: "Mutual critique", proposal: "Proposal", ballot: "Ballot", arbitration: "Fallback arbitration", final: "Final answer", collected: "Collected report" };

class ReadError extends Error {
 constructor(public status: number) { super(status === 501 ? "Knowledge graph is not configured on this host." : status === 404 ? "Consultation not found." : "Consultation index is unavailable. Try again."); }
}

function Metadata({ item }: { item: Summary }) {
 const values = [
  ["Requester", item.requester], ["Archive host", item.host], ["Recorded host", item.recordedHost],
  ["Project", item.project], ["Branch", item.branch], ["Commit", item.sha],
  ["Created", item.createdAt || item.date], ["Verified", item.verifiedAt], ["Phase", item.phase], ["Decision", item.decision],
  ["Archive", item.archive], ["Delivery", item.delivery], ["Closure", item.closure],
  ["Escalation", item.escalation], ["Decision process", item.mode], ["Last sync", item.syncedAt],
 ];
 return <dl className="grid grid-cols-1 gap-x-6 gap-y-1 text-sm @lg:grid-cols-2">
  {values.map(([label, value]) => <div key={label} className="grid min-w-0 grid-cols-[max-content_minmax(0,1fr)] gap-x-3"><dt className="text-muted-foreground">{label}</dt><dd className="break-words">{value || "Unknown"}</dd></div>)}
  {item.consulClosure?.map((closure) => <div key={closure.consul} className="grid min-w-0 grid-cols-[max-content_minmax(0,1fr)] gap-x-3"><dt className="text-muted-foreground">{closure.consul} closure</dt><dd className="break-words">{closure.status}</dd></div>)}
  {item.arbitration?.status && <div className="grid min-w-0 grid-cols-[max-content_minmax(0,1fr)] gap-x-3"><dt className="text-muted-foreground">Fallback arbitration</dt><dd className="break-words">{item.arbitration.status} · {item.arbitration.model || "Model unknown"} · {item.arbitration.effort || "Effort unknown"}</dd></div>}
 </dl>;
}

export function ConsultationEvidence({ detail }: { detail: Detail }) {
 return <>
  <h2 className="text-lg font-medium break-words">{detail.case || detail.name}</h2>
  <Metadata item={detail} />
  <p className="text-sm text-muted-foreground">{detail.partial ? "Partial evidence" : "Indexed evidence"} · {detail.rounds} observed ratification rounds · {detail.proposalVersions || 0} proposal versions · {detail.phaseRounds} phase rounds</p>
  {detail.warnings?.map((warning) => <p key={warning} role="status" className="text-sm text-muted-foreground">{warning}</p>)}
  <section aria-label="Consultation timeline" className="divide-y divide-border">
   {detail.artifacts?.map((artifact) => <details key={artifact.name} className="py-3" open={artifact.phase === "final"}>
    <summary className="cursor-pointer text-sm font-medium focus-visible:outline focus-visible:outline-ring">
     {phaseLabels[artifact.phase] || artifact.phase}{artifact.round ? ` · ${artifact.phase === "proposal" ? "Version" : "Round"} ${artifact.round}` : ""}{artifact.consul ? ` · ${artifact.consul}` : ""}
     {artifact.missing ? " · Missing evidence" : ""}{artifact.truncated ? " · Truncated" : ""}
    </summary>
    <div className="mt-3 min-w-0 space-y-3">
     {artifact.absentSince && <p role="status" className="text-sm text-muted-foreground">Absent since {artifact.absentSince}; last indexed content retained.</p>}
     {artifact.phase === "ballot" && <p className="text-sm">{artifact.ballots?.length ? artifact.ballots.map((vote) => `${vote.decision}: ${vote.stance}`).join(" · ") : "Ballot stance unknown"}</p>}
     {artifact.truncated && <p className="text-sm text-muted-foreground">Stored excerpt; full artifact hash retained.</p>}
     {artifact.content ? <ChatMarkdown text={artifact.content} safeOrigin /> : <p className="text-sm text-muted-foreground">{artifact.missing ? "No recorded artifact available." : "Artifact is empty."}</p>}
     {artifact.hash && <details className="text-caption text-muted-foreground"><summary className="cursor-pointer">Evidence identity</summary><p className="break-all font-mono">{artifact.name} · SHA-256 {artifact.hash}</p></details>}
    </div>
   </details>)}
  </section>
 </>;
}

export function ConsultationsView({ id }: { id?: string }) {
 const [cursors, setCursors] = useState([""]);
 const cursor = cursors[cursors.length - 1];
 const list = useQuery({
  queryKey: ["consultations", cursor], enabled: !id, retry: false, refetchInterval: 30_000,
  queryFn: async ({ signal }) => {
   const result = await apiClient.GET("/api/v1/knowledge/consultations", { params: { query: { limit: 25, cursor } }, signal });
   if (!result.data) throw new ReadError(result.response.status);
   return result.data;
  },
 });
 const detail = useQuery({
  queryKey: ["consultation", id], enabled: !!id, retry: false, refetchInterval: 30_000,
  queryFn: async ({ signal }) => {
   const result = await apiClient.GET("/api/v1/knowledge/consultations/{id}", { params: { path: { id: id! } }, signal });
   if (!result.data) throw new ReadError(result.response.status);
   return result.data;
  },
 });
 const active = id ? detail : list;
 return <main className="@container mx-auto flex min-h-0 w-full max-w-4xl flex-1 flex-col gap-4 overflow-y-auto p-5">
  <header className="flex items-center justify-between gap-3"><h1 className="text-xl font-semibold">Architecture consultations</h1><Button variant="ghost" size="sm" onClick={() => void active.refetch()}>Refresh</Button></header>
  {id && <Link to="/consultations" className="text-sm text-muted-foreground hover:text-foreground">All consultations</Link>}
  {active.isPending && <p role="status">Loading consultations…</p>}
  {active.error && <p role="alert" className="text-sm">{active.error.message}</p>}
  {id && detail.data && <ConsultationEvidence detail={detail.data} />}
  {!id && list.data && <>
   <p className="text-caption text-muted-foreground">Local archive scanner: {list.data.scan?.enabled ? (list.data.scan.lastScan ? `last scan ${list.data.scan.lastScan} · ${list.data.scan.imported} imported / ${list.data.scan.examined} examined · ${list.data.scan.durationMs} ms` : "awaiting first scan") : "disabled"}. Other hosts may still be unsynced.</p>
   {list.data.scan?.error && <p role="status" className="text-sm">{list.data.scan.error}</p>}
   {!list.data.items?.length ? <p role="status" className="text-sm text-muted-foreground">No indexed consultations. Archives appear after a participating host scans with its knowledge graph configured.</p> :
    <ul className="divide-y divide-border">{list.data.items.map((item) => <li key={item.id}>
     <Link to="/consultations/$id" params={{ id: item.id }} className="flex min-h-12 flex-col gap-1 py-3 focus-visible:outline focus-visible:outline-ring">
      <span className="font-medium break-words">{item.case || item.name}</span>
      <span className="text-sm text-muted-foreground">{item.requester || "Requester unknown"} · {item.host} · {item.date} · {item.decision || "Decision unknown"}{item.partial ? " · Partial evidence" : ""}</span>
     </Link>
    </li>)}</ul>}
   <nav aria-label="Consultation pages" className="flex gap-2">
    <Button variant="ghost" size="sm" disabled={cursors.length === 1} onClick={() => setCursors((current) => current.slice(0, -1))}>Previous</Button>
    <Button variant="ghost" size="sm" disabled={!list.data.nextCursor} onClick={() => setCursors((current) => [...current, list.data!.nextCursor])}>Next</Button>
   </nav>
  </>}
 </main>;
}

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient } from "../lib/api-client";
import { Button } from "./ui/button";
import { ChatMarkdown } from "./chat/ChatMarkdown";

type Detail = components["schemas"]["ConsultationDetail"];
type Summary = components["schemas"]["ConsultationSummary"];

class ReadError extends Error {
 constructor(public status: number) { super(); }
}

function Metadata({ item }: { item: Summary }) {
 const { t } = useTranslation();
 const values = [
  [t("consultations.requester"), item.requester], [t("consultations.archiveHost"), item.host], [t("consultations.recordedHost"), item.recordedHost],
  [t("consultations.project"), item.project], [t("consultations.branch"), item.branch], [t("consultations.commit"), item.sha],
  [t("consultations.created"), item.createdAt || item.date], [t("consultations.verified"), item.verifiedAt], [t("consultations.phase"), item.phase], [t("consultations.decision"), item.decision],
  [t("consultations.archive"), item.archive], [t("consultations.delivery"), item.delivery], [t("consultations.closure"), item.closure],
  [t("consultations.escalation"), item.escalation], [t("consultations.process"), item.mode], [t("consultations.lastSync"), item.syncedAt],
  ...[[t("consultations.patchDigest"), item.patchDigest], [t("consultations.escalated"), item.escalatedAt], [t("consultations.deadline"), item.escalationDeadline], [t("consultations.responded"), item.respondedAt]].filter(([, value]) => Boolean(value)),
 ];
 return <dl className="grid grid-cols-1 gap-x-6 gap-y-1 text-sm @lg:grid-cols-2">
  {values.map(([label, value]) => <div key={label} className="grid min-w-0 grid-cols-[max-content_minmax(0,1fr)] gap-x-3"><dt className="text-muted-foreground">{label}</dt><dd className="break-words">{value || t("consultations.unknown")}</dd></div>)}
  {item.consulClosure?.map((closure) => <div key={closure.consul} className="grid min-w-0 grid-cols-[max-content_minmax(0,1fr)] gap-x-3"><dt className="text-muted-foreground">{t("consultations.consulClosure", { consul: closure.consul })}</dt><dd className="break-words">{closure.status}</dd></div>)}
  {item.arbitration?.status && <div className="grid min-w-0 grid-cols-[max-content_minmax(0,1fr)] gap-x-3"><dt className="text-muted-foreground">{t("consultations.arbitration")}</dt><dd className="break-words">{item.arbitration.status} · {item.arbitration.model || t("consultations.modelUnknown")} · {item.arbitration.effort || t("consultations.effortUnknown")} · {item.arbitration.host || t("consultations.hostUnknown")}</dd></div>}
 </dl>;
}

export function ConsultationEvidence({ detail }: { detail: Detail }) {
 const { t } = useTranslation();
 const phaseLabels: Record<string, string> = { request: t("consultations.request"), independent: t("consultations.independent"), critique: t("consultations.critique"), proposal: t("consultations.proposal"), ballot: t("consultations.ballot"), arbitration: t("consultations.arbitration"), final: t("consultations.final"), collected: t("consultations.collected") };
 return <>
  <h2 className="text-lg font-medium break-words">{detail.case || detail.name}</h2>
  <Metadata item={detail} />
  <p className="text-sm text-muted-foreground">{t(detail.partial ? "consultations.partial" : "consultations.indexed")} · {t("consultations.roundCounts", { rounds: detail.rounds, proposals: detail.proposalVersions || 0, phases: detail.phaseRounds })}</p>
  {detail.warnings?.map((warning) => <p key={warning} role="status" className="text-sm text-muted-foreground">{warning}</p>)}
  <section aria-label={t("consultations.timeline")} className="divide-y divide-border">
   {detail.artifacts?.map((artifact) => <details key={artifact.name} className="py-3" open={artifact.phase === "final"}>
    <summary className="cursor-pointer text-sm font-medium focus-visible:outline focus-visible:outline-ring">
     {phaseLabels[artifact.phase] || artifact.phase}{artifact.round ? ` · ${t(artifact.phase === "proposal" ? "consultations.version" : "consultations.round")} ${artifact.round}` : ""}{artifact.consul ? ` · ${artifact.consul}` : ""}
     {artifact.missing ? ` · ${t("consultations.missing")}` : artifact.unavailable ? ` · ${t("consultations.unavailable")}` : ""}{artifact.truncated ? ` · ${t("consultations.truncated")}` : ""}
    </summary>
    <div className="mt-3 min-w-0 space-y-3">
     {artifact.unavailable && <p role="status" className="text-sm text-muted-foreground">{t("consultations.unsafe")}{artifact.hash ? ` ${t("consultations.retained")}` : ""}</p>}
     {artifact.absentSince && <p role="status" className="text-sm text-muted-foreground">{t("consultations.absentSince", { at: artifact.absentSince })}</p>}
     {artifact.phase === "ballot" && <p className="text-sm">{artifact.ballots?.length ? artifact.ballots.map((vote) => `${vote.decision}: ${vote.stance}`).join(" · ") : t("consultations.ballotUnknown")}</p>}
     {artifact.truncated && <p className="text-sm text-muted-foreground">{t("consultations.excerpt")}</p>}
     {artifact.content ? <ChatMarkdown text={artifact.content} safeOrigin /> : <p className="text-sm text-muted-foreground">{artifact.missing ? t("consultations.noArtifact") : t("consultations.emptyArtifact")}</p>}
     {artifact.hash && <details className="text-caption text-muted-foreground"><summary className="cursor-pointer">{t("consultations.identity")}</summary><p className="break-all font-mono">{t("consultations.hash", { name: artifact.name, hash: artifact.hash })}</p></details>}
    </div>
   </details>)}
  </section>
 </>;
}

export function ConsultationsView({ id }: { id?: string }) {
 const { t } = useTranslation();
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
  <header className="flex items-center justify-between gap-3"><h1 className="text-xl font-semibold">{t("consultations.title")}</h1><Button variant="ghost" size="sm" onClick={() => void active.refetch()}>{t("consultations.refresh")}</Button></header>
  {id && <Link to="/consultations" className="text-sm text-muted-foreground hover:text-foreground">{t("consultations.all")}</Link>}
  {active.isPending && <p role="status">{t("consultations.loading")}</p>}
  {active.error && <p role="alert" className="text-sm">{t(active.error instanceof ReadError && active.error.status === 501 ? "consultations.notConfigured" : active.error instanceof ReadError && active.error.status === 404 ? "consultations.notFound" : "consultations.readError")}</p>}
  {id && detail.data && <ConsultationEvidence detail={detail.data} />}
  {!id && list.data && <>
   <p className="text-caption text-muted-foreground">{t("consultations.scanner", { status: list.data.scan?.enabled ? (list.data.scan.lastScan ? t("consultations.scanStats", { at: list.data.scan.lastScan, imported: list.data.scan.imported, examined: list.data.scan.examined, ms: list.data.scan.durationMs }) : t("consultations.awaitingScan")) : t("consultations.disabled") })}</p>
   {list.data.scan?.error && <p role="status" className="text-sm">{list.data.scan.error}</p>}
   {!list.data.items?.length ? <p role="status" className="text-sm text-muted-foreground">{t("consultations.empty")}</p> :
    <ul className="divide-y divide-border">{list.data.items.map((item) => <li key={item.id}>
     <Link to="/consultations/$id" params={{ id: item.id }} className="flex min-h-12 flex-col gap-1 py-3 focus-visible:outline focus-visible:outline-ring">
      <span className="font-medium break-words">{item.case || item.name}</span>
      <span className="text-sm text-muted-foreground">{item.requester || t("consultations.requesterUnknown")} · {item.host} · {item.date} · {item.decision || t("consultations.decisionUnknown")}{item.partial ? ` · ${t("consultations.partial")}` : ""}</span>
     </Link>
    </li>)}</ul>}
   <nav aria-label={t("consultations.pages")} className="flex gap-2">
    <Button variant="ghost" size="sm" disabled={cursors.length === 1} onClick={() => setCursors((current) => current.slice(0, -1))}>{t("consultations.previous")}</Button>
    <Button variant="ghost" size="sm" disabled={!list.data.nextCursor} onClick={() => setCursors((current) => [...current, list.data!.nextCursor])}>{t("consultations.next")}</Button>
   </nav>
  </>}
 </main>;
}

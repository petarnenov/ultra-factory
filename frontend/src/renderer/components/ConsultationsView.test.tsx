import { I18nextProvider } from "react-i18next";
import { createFullAppI18n } from "../i18n/all-messages";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import type { components } from "../../api/schema";
import { ConsultationEvidence, ConsultationsView } from "./ConsultationsView";

const { get } = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("../lib/api-client", () => ({ apiClient: { GET: get } }));
vi.mock("@tanstack/react-router", () => ({ Link: ({ children, to }: { children: React.ReactNode; to: string }) => <a href={to}>{children}</a>, useRouterState: () => undefined }));

function show(id?: string) {
 const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
 return render(<QueryClientProvider client={client}><ConsultationsView id={id} /></QueryClientProvider>);
}
beforeEach(() => get.mockReset());
it("shows loading and explains an empty index and local scanner failure", async () => {
 get.mockResolvedValue({ data: { items: [], scan: { enabled: true, error: "Import failed" } } });
 show();
 expect(screen.getByText("Loading consultations…")).toBeInTheDocument();
 expect(await screen.findByText(/No indexed consultations/)).toBeInTheDocument();
 expect(screen.getByText("Import failed")).toBeInTheDocument();
});
it.each([[501, "Knowledge graph is not configured"], [503, "Consultation index is unavailable"], [404, "Consultation not found"]])("shows unavailable %s and retries", async (status, text) => {
 get.mockResolvedValue({ response: { status } });
 show(status === 404 ? "id" : undefined);
 expect(await screen.findByRole("alert")).toHaveTextContent(text);
 await userEvent.click(screen.getByRole("button", { name: "Refresh" }));
 expect(get).toHaveBeenCalledTimes(2);
});
it("paginates indexed records", async () => {
 get.mockResolvedValue({ data: { items: [{ id: "opaque", name: "Case", host: "host", date: "2026-10-23" }], nextCursor: "cursor", scan: {} } });
 show();
 expect(await screen.findByText("Case")).toBeInTheDocument();
 await userEvent.click(screen.getByRole("button", { name: "Next" }));
 expect(get).toHaveBeenLastCalledWith("/api/v1/knowledge/consultations", expect.objectContaining({ params: { query: { limit: 25, cursor: "cursor" } } }));
});
it("shows partial evidence, separate statuses and safely renders original Markdown", async () => {
 const detail = {
  name: "Case", partial: true, decision: "unresolved", archive: "verified", delivery: "unconfirmed", closure: "not_started", mode: "d03_fallback", rounds: 0, phaseRounds: 1,
  artifacts: [{ name: "final.md", phase: "final", content: "# Original answer\n[local](file:///secret)\n[relative](../../secret)\n<script>alert(1)</script>", hash: "abc", truncated: true }, { name: "ballots/round-1/astra.md", phase: "ballot", missing: true, ballots: [] }],
 } as unknown as components["schemas"]["ConsultationDetail"];
 const { container } = render(<ConsultationEvidence detail={detail} />);
 expect(screen.getByText("unresolved")).toBeInTheDocument();
 expect(screen.getByText("unconfirmed")).toBeInTheDocument();
 expect(screen.getByText("d03_fallback")).toBeInTheDocument();
 expect(screen.getByText(/Partial evidence/)).toBeInTheDocument();
 expect(screen.getByText(/Ballot stance unknown/)).toBeInTheDocument();
 expect(screen.getByRole("heading", { name: "Original answer" })).toBeInTheDocument();
 expect(container.querySelector('a[href^="file:"], a[href^=".."], script')).toBeNull();
});
it("loads a detail using its opaque identifier", async () => {
 get.mockResolvedValue({ data: { name: "Detail case", artifacts: [], rounds: 0, phaseRounds: 0 } });
 show("opaque");
 expect(await screen.findByText("Detail case")).toBeInTheDocument();
 expect(get).toHaveBeenCalledWith("/api/v1/knowledge/consultations/{id}", expect.objectContaining({ params: { path: { id: "opaque" } } }));
});

it.each([["fable", "claude-fable-5-1"], ["astra", "gpt-6-astra"], ["deepseek", "opencode/deepseek-v4.1-flash"]])("labels %s fallback arbitration without council votes", (consul, model) => {
 const detail = {
  name: "Arbitration case", partial: true, rounds: 0, proposalVersions: 1, phaseRounds: 0,
  archive: "verified", delivery: "unconfirmed", mode: "fallback_arbitration",
  consulClosure: [{ consul: "deepseek", status: "not_started" }],
  arbitration: { status: "completed", model, effort: "medium" },
  artifacts: [{ name: `fallback-arbitration/${consul}.md`, phase: "arbitration", consul, content: "Original arbiter ruling", ballots: [] }],
 } as unknown as components["schemas"]["ConsultationDetail"];
 render(<ConsultationEvidence detail={detail} />);
 expect(screen.getByText(/0 observed ratification rounds · 1 proposal versions/)).toBeInTheDocument();
 expect(screen.getByText("not_started")).toBeInTheDocument();
 expect(screen.getByText("Original arbiter ruling")).toBeInTheDocument();
 expect(screen.getByText(`Fallback arbitration · ${consul}`)).toBeInTheDocument();
 expect(screen.queryByText("Ballot stance unknown")).not.toBeInTheDocument();
});

it("displays the explicit requester field from future consultation archives", () => {
 const detail = { name: "Future consultation", requester: "Recorded engineer", artifacts: [], rounds: 0, proposalVersions: 0, phaseRounds: 0 } as unknown as components["schemas"]["ConsultationDetail"];
 render(<ConsultationEvidence detail={detail} />);
 expect(screen.getByText("Requester")).toBeInTheDocument();
 expect(screen.getByText("Recorded engineer")).toBeInTheDocument();
});

it.each([["es", "Arbitraje alternativo", "Solicitante", "Desconocido"], ["de", "Ersatzschlichtung", "Anfragende Person", "Unbekannt"]] as const)("localizes %s chrome while preserving recorded evidence and unknown requester", (locale, arbitration, requester, unknown) => {
 const detail = { name: "Recorded archive", requester: "", artifacts: [{ name: "fallback-arbitration/fable.md", phase: "arbitration", consul: "fable", content: "Original arbiter evidence", ballots: [] }], rounds: 0, proposalVersions: 1, phaseRounds: 1 } as unknown as components["schemas"]["ConsultationDetail"];
 render(<I18nextProvider i18n={createFullAppI18n(locale)}><ConsultationEvidence detail={detail} /></I18nextProvider>);
 expect(screen.getByText(requester)).toBeInTheDocument();
 expect(screen.getAllByText(unknown).length).toBeGreaterThan(0);
 expect(screen.getByText(`${arbitration} · fable`)).toBeInTheDocument();
 expect(screen.getByText("Original arbiter evidence")).toBeInTheDocument();
 expect(screen.queryByText("Fallback arbitration · fable")).not.toBeInTheDocument();
});

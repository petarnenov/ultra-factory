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

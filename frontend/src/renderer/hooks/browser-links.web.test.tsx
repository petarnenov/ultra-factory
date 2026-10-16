import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useUiStore } from "../stores/ui-store";
import type { WorkspaceSession } from "../types/workspace";
import { useOpenArtifactPreview } from "./useOpenArtifactPreview";
import { useSessionBrowserLink } from "./useSessionBrowserLink";

const { postMock } = vi.hoisted(() => ({ postMock: vi.fn() }));

vi.mock("../lib/platform", async (importOriginal) =>
	(await import("../test/web-build-platform")).webBuildPlatform(await importOriginal()));
vi.mock("../lib/host-clients", () => ({
	clientForSessionHost: () => ({ GET: vi.fn(), POST: postMock }),
}));

const session = { id: "sess-1", status: "working" } as WorkspaceSession;

function wrapper({ children }: { children: ReactNode }) {
	return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;
}

let openMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
	postMock.mockReset().mockResolvedValue({ data: {}, error: undefined });
	openMock = vi.fn();
	vi.stubGlobal("open", openMock);
});

afterEach(() => {
	vi.unstubAllGlobals();
});

describe("browser links in the web build", () => {
	it("opens a web link in a new tab instead of the Browser panel", () => {
		const openInBrowser = vi.fn(async () => undefined);
		const { result } = renderHook(() => useSessionBrowserLink(session, openInBrowser, []), { wrapper });

		result.current("https://example.com/docs");

		expect(openMock).toHaveBeenCalledWith("https://example.com/docs", "_blank", "noopener,noreferrer");
		expect(openInBrowser).not.toHaveBeenCalled();
		expect(postMock).not.toHaveBeenCalled();
		expect(useUiStore.getState().inspectorSessions["sess-1"]?.view).not.toBe("browser");
	});

	it("opens a workspace file through the daemon attachment URL in a new tab", () => {
		const { result } = renderHook(() => useSessionBrowserLink(session, vi.fn(), ["reports/index.html"]), { wrapper });

		result.current("reports/index.html");

		expect(openMock).toHaveBeenCalledOnce();
		expect(openMock.mock.calls[0]?.[0]).toContain("reports/index.html");
		expect(openMock.mock.calls[0]?.[1]).toBe("_blank");
		expect(postMock).not.toHaveBeenCalled();
	});

	it("opens an artifact preview in a new tab", () => {
		const { result } = renderHook(() => useOpenArtifactPreview("sess-1"), { wrapper });

		result.current("http://ao-preview-artifact.sess-1.localhost/report.html");

		expect(openMock).toHaveBeenCalledWith("http://ao-preview-artifact.sess-1.localhost/report.html", "_blank", "noopener,noreferrer");
		expect(postMock).not.toHaveBeenCalled();
	});
});

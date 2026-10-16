import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { getMock, copyWorkspacePathMock, webBuild } = vi.hoisted(() => ({
	getMock: vi.fn(),
	copyWorkspacePathMock: vi.fn(),
	webBuild: { enabled: false },
}));

vi.mock("./api-client", async (importOriginal) => ({
	...await importOriginal<typeof import("./api-client")>(),
	apiClient: { GET: getMock },
}));
vi.mock("./bridge", async (importOriginal) => {
	const actual = await importOriginal<typeof import("./bridge")>();
	return { ...actual, aoBridge: { ...actual.aoBridge, editorHandoff: { ...actual.aoBridge.editorHandoff, copyWorkspacePath: copyWorkspacePathMock } } };
});
vi.mock("./platform", async (importOriginal) => {
	const actual = await importOriginal<typeof import("./platform")>();
	const web = (await import("../test/web-build-platform")).webBuildPlatform(actual);
	return {
		...actual,
		get capabilities() {
			return webBuild.enabled ? web.capabilities : actual.capabilities;
		},
	};
});

const { copySessionWorkspacePath } = await import("./session-workspace-path");

const writeText = vi.fn(async () => undefined);

beforeEach(() => {
	getMock.mockReset();
	copyWorkspacePathMock.mockReset();
	writeText.mockClear();
	Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
});

afterEach(() => {
	webBuild.enabled = false;
});

describe("copySessionWorkspacePath", () => {
	it("hands the copy to Electron main in the desktop app", async () => {
		copyWorkspacePathMock.mockResolvedValue(undefined);

		await copySessionWorkspacePath("ao-1");

		expect(copyWorkspacePathMock).toHaveBeenCalledWith("ao-1");
		expect(getMock).not.toHaveBeenCalled();
	});

	it("reads the path from the daemon and copies it in the web build", async () => {
		webBuild.enabled = true;
		getMock.mockResolvedValue({ data: { sessionId: "ao-1", workspacePath: "/data/worktrees/ao-1" }, error: undefined });

		await copySessionWorkspacePath("ao-1");

		expect(getMock).toHaveBeenCalledWith("/api/v1/desktop/sessions/{sessionId}/workspace", { params: { path: { sessionId: "ao-1" } } });
		expect(writeText).toHaveBeenCalledWith("/data/worktrees/ao-1");
		expect(copyWorkspacePathMock).not.toHaveBeenCalled();
	});

	it("reports a daemon error instead of copying in the web build", async () => {
		webBuild.enabled = true;
		getMock.mockResolvedValue({ data: undefined, error: { message: "Unknown session" } });

		await expect(copySessionWorkspacePath("ao-1")).rejects.toThrow("Unknown session");
		expect(writeText).not.toHaveBeenCalled();
	});
});

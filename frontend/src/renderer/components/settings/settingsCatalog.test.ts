import { describe, expect, it, vi } from "vitest";
const platform = vi.hoisted(() => ({ web: false }));
vi.mock("../../lib/platform", async (importOriginal) => {
	const actual = await importOriginal<typeof import("../../lib/platform")>();
	return { ...actual, get isWebBuild() { return platform.web; } };
});
import { visibleGlobalSettings } from "./settingsCatalog";

describe("visibleGlobalSettings in the desktop app", () => {
	it("lists Updates and Browser profiles but not Workers", () => {
		const sections = visibleGlobalSettings({ cloudEnabled: false, developerMode: false, diagnostics: false, hasCoder: false, selfServeBilling: false }).map((item) => item.id);

		expect(sections).toContain("updates");
		expect(sections).toContain("browserProfiles");
		expect(sections).not.toContain("workers");
	});
});

it("hides Codex account settings and shows Remote hosts in web builds", () => {
	platform.web = true;
	const sections = visibleGlobalSettings({ cloudEnabled: false, developerMode: false, diagnostics: false, hasCoder: false, selfServeBilling: false }).map((item) => item.id);
	expect(sections).not.toContain("agents");
	expect(sections).toContain("remoteHosts");
	platform.web = false;
});

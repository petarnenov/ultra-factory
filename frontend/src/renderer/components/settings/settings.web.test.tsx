import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { aoBridge } from "../../lib/bridge";
import { CliLinkSettings } from "./CliLinkSettingsRow";
import { visibleGlobalSettings } from "./settingsCatalog";

vi.mock("../../lib/platform", async (importOriginal) =>
	(await import("../../test/web-build-platform")).webBuildPlatform(await importOriginal()));

vi.mock("../../lib/bridge", async (importOriginal) => {
	const actual = await importOriginal<typeof import("../../lib/bridge")>();
	return { ...actual, aoBridge: { ...actual.aoBridge, cliLink: { setEnabled: vi.fn(async () => undefined), inspect: vi.fn(), link: vi.fn(), unlink: vi.fn() } } };
});

const context = { cloudEnabled: false, developerMode: true, diagnostics: false, hasCoder: false, selfServeBilling: false };

describe("Settings in the web build", () => {
	it("hides Updates and Browser profiles but keeps Remote hosts and adds Workers", () => {
		const sections = visibleGlobalSettings(context).map((item) => item.id);

		expect(sections).not.toContain("updates");
		expect(sections).not.toContain("browserProfiles");
		expect(sections).toContain("remoteHosts");
		expect(sections).toContain("workers");
		expect(sections).toContain("general");
	});

	it("hides the CLI link row even in Developer mode", () => {
		const { container } = render(<CliLinkSettings enabled />);

		expect(container).toBeEmptyDOMElement();
		expect(aoBridge.cliLink.setEnabled).not.toHaveBeenCalled();
		expect(aoBridge.cliLink.inspect).not.toHaveBeenCalled();
	});
});

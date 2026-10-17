import { describe, expect, it } from "vitest";
import { visibleGlobalSettings } from "./settingsCatalog";

describe("visibleGlobalSettings in the desktop app", () => {
	it("lists Updates and Browser profiles but not Workers", () => {
		const sections = visibleGlobalSettings({ cloudEnabled: false, developerMode: false, diagnostics: false, hasCoder: false, selfServeBilling: false }).map((item) => item.id);

		expect(sections).toContain("updates");
		expect(sections).toContain("browserProfiles");
		expect(sections).not.toContain("workers");
	});
});

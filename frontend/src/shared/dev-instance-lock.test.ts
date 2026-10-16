import { describe, expect, it } from "vitest";
import { devInstanceLockMessage, singletonLockHolderPid } from "./dev-instance-lock";

describe("singletonLockHolderPid", () => {
	it("reads the pid from a <hostname>-<pid> lock target", () => {
		expect(singletonLockHolderPid("Petars-MacBook-Air.local-67135")).toBe(67135);
	});

	it("returns null for a missing or unparseable target", () => {
		expect(singletonLockHolderPid(null)).toBeNull();
		expect(singletonLockHolderPid("garbage")).toBeNull();
	});
});

describe("devInstanceLockMessage", () => {
	it("names the holder pid and both ways out", () => {
		const message = devInstanceLockMessage("/Users/me/.ao/dev/electron", 67135);
		expect(message).toContain("(pid 67135)");
		expect(message).toContain("/Users/me/.ao/dev/electron");
		expect(message).toContain("kill 67135");
		expect(message).toContain("AO_DEV_ELECTRON_DIR");
	});

	it("still explains the fix when the holder is unknown", () => {
		const message = devInstanceLockMessage("/Users/me/.ao/dev/electron", null);
		expect(message).not.toContain("pid");
		expect(message).toContain("AO_DEV_ELECTRON_DIR");
	});
});

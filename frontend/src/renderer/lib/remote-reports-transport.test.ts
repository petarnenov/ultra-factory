import { expect, it } from "vitest";
import { needsPlainHTTPOptIn } from "./remote-reports-transport";

it("shows the plain-http switch only for addresses that are neither https, loopback, nor Tailscale", () => {
	for (const [address, want] of [
		["https://build.example", false],
		["http://127.0.0.1:3001", false],
		["http://localhost:3001", false],
		["http://[::1]:3001", false],
		["http://100.106.144.10:3011", false],
		["http://z600.tail1234.ts.net:3011", false],
		["http://[fd7a:115c:a1e0::5]:3011", false],
		["http://192.168.0.164:3011", true],
		["http://100.128.0.1:3011", true],
		["192.168.0.164:3011", true],
		["http://office.example", true],
	] as const) {
		expect(needsPlainHTTPOptIn(address), address).toBe(want);
	}
});

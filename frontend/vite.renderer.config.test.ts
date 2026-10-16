// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { contentSecurityPolicy } from "./vite.renderer.csp";

describe("packaged renderer CSP", () => {
	it("frames agent renders from the loopback daemon and nothing else", () => {
		const frameSrc = contentSecurityPolicy("build")
			.split("; ")
			.filter((directive) => directive.startsWith("frame-src"));
		expect(frameSrc).toEqual(["frame-src 'self' http://127.0.0.1:* http://*.localhost:*"]);
	});
});

describe("web build renderer CSP", () => {
	const directive = (policy: string, name: string) =>
		policy.split("; ").find((entry) => entry.startsWith(`${name} `));

	afterEach(() => {
		vi.unstubAllEnvs();
	});

	it("keeps every connection and frame same-origin, with no loopback origins", () => {
		const policy = contentSecurityPolicy("build", true);
		expect(policy).not.toContain("127.0.0.1");
		expect(policy).not.toContain("localhost");
		expect(directive(policy, "connect-src")?.split(" ").slice(0, 2)).toEqual(["connect-src", "'self'"]);
		expect(directive(policy, "connect-src")).not.toMatch(/ (https?|wss?):( |$)/);
		expect(directive(policy, "img-src")).toBe("img-src 'self' data: https:");
		expect(directive(policy, "frame-src")).toBe("frame-src 'self'");
		expect(directive(policy, "script-src")).toBe("script-src 'self'");
	});

	it("selects the web policy from VITE_AO_WEB", () => {
		vi.stubEnv("VITE_AO_WEB", "1");
		expect(contentSecurityPolicy("build")).toBe(contentSecurityPolicy("build", true));
		vi.stubEnv("VITE_AO_WEB", "");
		expect(contentSecurityPolicy("build")).toContain("http://127.0.0.1:*");
	});

	it("allows exactly an explicit API origin and its websocket twin", () => {
		vi.stubEnv("VITE_AO_API_BASE_URL", "https://ao.example.test/base");
		const connectSrc = directive(contentSecurityPolicy("build", true), "connect-src")?.split(" ");
		expect(connectSrc).toContain("https://ao.example.test");
		expect(connectSrc).toContain("wss://ao.example.test");
	});
});


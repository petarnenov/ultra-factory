import { readFileSync } from "node:fs";
import path from "node:path";
import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";

// login.js is a plain browser script, so it runs here in a VM with just the
// DOM, storage, and navigation it touches.
const LOGIN_JS = readFileSync(path.join(import.meta.dirname, "..", "login", "login.js"), "utf8");
const ORIGIN = "https://hub.example.test";
const RETURN_TO_KEY = "ao.hub.returnTo";

async function signIn(stored: string | null, status = 204): Promise<{ replacedWith: string[]; storage: Map<string, string> }> {
	const storage = new Map<string, string>(stored === null ? [] : [[RETURN_TO_KEY, stored]]);
	const replacedWith: string[] = [];
	let submit: ((event: { preventDefault: () => void }) => Promise<void>) | undefined;
	const element = () => ({ hidden: true, textContent: "", disabled: false, setAttribute() {}, focus() {} });
	const button = element();
	const elements: Record<string, unknown> = {
		login: { ...element(), querySelector: () => button, addEventListener: (_: string, handler: typeof submit) => { submit = handler; } },
		password: { ...element(), value: "operator-password" },
		error: element(),
	};
	const window = {
		location: { origin: ORIGIN, replace: (url: string) => replacedWith.push(url) },
		sessionStorage: {
			getItem: (key: string) => storage.get(key) ?? null,
			removeItem: (key: string) => storage.delete(key),
		},
	};
	runInNewContext(LOGIN_JS, {
		window,
		document: { getElementById: (id: string) => elements[id] },
		fetch: async () => ({ ok: status >= 200 && status < 300, status }),
		URL,
	});
	await submit?.({ preventDefault() {} });
	return { replacedWith, storage };
}

describe("login page return address", () => {
	it("returns to the address the signed-out tab was on, hash route included", async () => {
		const { replacedWith, storage } = await signIn("/?tab=1#/projects/ultra-factory/sessions/ultra-factory-14");
		expect(replacedWith).toEqual(["/?tab=1#/projects/ultra-factory/sessions/ultra-factory-14"]);
		expect(storage.has(RETURN_TO_KEY)).toBe(false);
	});

	it("goes home when no address was saved", async () => {
		expect((await signIn(null)).replacedWith).toEqual(["/"]);
	});

	it.each([
		["another site", "https://evil.example/"],
		["a protocol-relative URL", "//evil.example/"],
		["a backslash URL", "/\\evil.example/"],
		["a tab the URL parser strips", "/\t/evil.example/"],
		["a newline the URL parser strips", "/\n/evil.example/"],
		["the login page itself", "/login"],
		["the login page with a query", "/login?x=1"],
	])("goes home instead of following %s", async (_name, stored) => {
		expect((await signIn(stored)).replacedWith).toEqual(["/"]);
	});

	it("keeps the saved address when the password is wrong", async () => {
		const { replacedWith, storage } = await signIn("/#/projects/ultra-factory", 401);
		expect(replacedWith).toEqual([]);
		expect(storage.get(RETURN_TO_KEY)).toBe("/#/projects/ultra-factory");
	});
});

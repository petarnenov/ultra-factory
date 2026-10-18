import { afterEach, expect, it } from "vitest";
import { HUB_RETURN_TO_KEY, isHubSignedOut, redirectToHubLogin } from "./hub-login";

afterEach(() => {
	window.sessionStorage.clear();
	window.history.replaceState(null, "", "/");
});

it("recognizes only the hub's own sign-out answer", () => {
	expect(isHubSignedOut(401, { error: "unauthorized" })).toBe(true);
	expect(isHubSignedOut(401, { error: "invalid_credentials" })).toBe(false);
	expect(isHubSignedOut(401, null)).toBe(false);
	expect(isHubSignedOut(502, { error: "unauthorized" })).toBe(false);
});

it("remembers the full address, hash route included, before leaving for /login", () => {
	window.history.replaceState(null, "", "/?tab=1#/projects/ultra-factory/sessions/ultra-factory-14");
	redirectToHubLogin();
	expect(window.sessionStorage.getItem(HUB_RETURN_TO_KEY)).toBe("/?tab=1#/projects/ultra-factory/sessions/ultra-factory-14");
});

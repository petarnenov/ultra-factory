// The hub BFF keeps sessions in memory, so a redeploy signs an open tab out
// without a page load; this sends it to /login, remembering where it was.

/** Shared with packages/hub-bff/login/login.js, which reads and clears it. */
export const HUB_RETURN_TO_KEY = "ao.hub.returnTo";

/** The BFF's answer to a request without a valid session. */
export function isHubSignedOut(status: number, body: unknown): boolean {
	return status === 401 && (body as { error?: unknown } | null)?.error === "unauthorized";
}

export function redirectToHubLogin(): void {
	const { pathname, search, hash } = window.location;
	try {
		window.sessionStorage.setItem(HUB_RETURN_TO_KEY, `${pathname}${search}${hash}`);
	} catch {
		// Storage can be blocked; signing in then lands on the home page.
	}
	window.location.replace("/login");
}

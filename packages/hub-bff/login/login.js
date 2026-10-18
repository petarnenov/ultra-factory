// Posts the operator password as JSON to the BFF. On success the BFF has set
// the HttpOnly session cookie, so the page just navigates to the SPA, back to
// the address a signed-out tab was on when the SPA sent it here.
const form = document.getElementById("login");
const input = document.getElementById("password");
const error = document.getElementById("error");
const button = form.querySelector("button");

// Written by the SPA (frontend/src/renderer/lib/hub-login.ts).
const RETURN_TO_KEY = "ao.hub.returnTo";

const MESSAGES = {
	401: "Incorrect password.",
	429: "Too many attempts. Wait a minute and try again.",
	503: "The server is busy. Try again in a moment.",
};

form.addEventListener("submit", async (event) => {
	event.preventDefault();
	if (!input.value) {
		showError("Enter the operator password.");
		return;
	}
	button.disabled = true;
	error.hidden = true;
	try {
		const res = await fetch("/login", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ password: input.value }),
			credentials: "same-origin",
		});
		if (res.ok) {
			window.location.replace(takeReturnTo());
			return;
		}
		showError(MESSAGES[res.status] ?? `Sign-in failed (HTTP ${res.status}).`);
	} catch {
		showError("Could not reach the server. Check the connection and try again.");
	} finally {
		button.disabled = false;
	}
});

function showError(message) {
	error.textContent = message;
	error.hidden = false;
	input.setAttribute("aria-invalid", "true");
	input.focus();
}

// Only an address on this origin is followed. Resolving it the way the
// browser will rules out another site ("//host", "/\\host", or one hidden
// behind a tab or newline the URL parser strips) and the login page itself.
function takeReturnTo() {
	let target = null;
	try {
		target = window.sessionStorage.getItem(RETURN_TO_KEY);
		window.sessionStorage.removeItem(RETURN_TO_KEY);
	} catch {
		// Storage can be blocked; fall back to the home page.
	}
	if (typeof target !== "string" || !target.startsWith("/")) return "/";
	let url;
	try {
		url = new URL(target, window.location.origin);
	} catch {
		return "/";
	}
	if (url.origin !== window.location.origin || url.pathname === "/login") return "/";
	return `${url.pathname}${url.search}${url.hash}`;
}

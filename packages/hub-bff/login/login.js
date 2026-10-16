// Posts the operator password as JSON to the BFF. On success the BFF has set
// the HttpOnly session cookie, so the page just navigates to the SPA.
const form = document.getElementById("login");
const input = document.getElementById("password");
const error = document.getElementById("error");
const button = form.querySelector("button");

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
			window.location.replace("/");
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

// Renderer CSP, kept out of vite.renderer.config.ts so that config's only
// export is its default (a named export alongside it trips Rolldown's
// MIXED_EXPORTS warning when Forge bundles the config).
import { DEFAULT_POSTHOG_HOST } from "./src/shared/posthog-config";

const POSTHOG_ORIGINS = (() => {
	const configured = process.env.VITE_AO_POSTHOG_HOST?.trim() || DEFAULT_POSTHOG_HOST;
	if (!configured) return [];
	let url: URL;
	try {
		url = new URL(configured);
	} catch {
		return [];
	}
	// posthog-js serves capture from api_host but fetches remote config from a
	// sibling "-assets" host it derives from the same name, so a CSP built only
	// from api_host blocks that request and logs a console error on every launch
	// of a packaged build. Capture is unaffected (it uses api_host), and AO
	// ignores what remote config offers, since replay, flags, and surveys are all
	// disabled in the client. Allowing the origin only silences the error; the
	// client settings still win over anything the server would say.
	//
	// The asset_host option deliberately does not cover this: per its own docs it
	// "only applies to /static/* asset paths; dynamic assets like remote config
	// continue to use the regular asset host derived from api_host".
	// Scoped to PostHog Cloud, matching what posthog-js itself does: it only
	// rewrites to an "-assets" sibling for *.posthog.com. A self-hosted instance
	// or a loopback capture endpoint serves everything from one origin, and
	// deriving there would emit a nonsense entry (127.0.0.1 would become
	// "127-assets.0.0.1").
	const origins = [url.origin];
	if (/\.posthog\.com$/i.test(url.hostname)) {
		const assetsHost = url.hostname.replace(/^([^.]+)\./, "$1-assets.");
		if (assetsHost !== url.hostname) origins.push(`${url.protocol}//${assetsHost}`);
	}
	return origins;
})();

// Cloud terminals attach over a ticketed wss:// dialed directly from the
// renderer (the WorkOS token stays in the main process; the single-use ticket
// is the socket's whole authorization — see lib/cloud-terminal-mux.ts), so the
// packaged CSP must allow the control-plane origin or every cloud terminal
// stays on "Connecting…" forever in built apps. The runtime URL is a daemon
// setting the build cannot read; list the baked defaults and fold in a
// developer's AO_CLOUD_CONTROL_PLANE_URL override so a custom control plane
// keeps a working terminal in packaged builds too.
const CLOUD_CP_WS_ORIGINS = (() => {
	const origins = ["wss://staging-api.aoagents.dev", "wss://api.aoagents.dev"];
	const override = process.env.AO_CLOUD_CONTROL_PLANE_URL?.trim();
	if (!override) return origins;
	let url: URL;
	try {
		url = new URL(override);
	} catch {
		return origins;
	}
	const origin = `${url.protocol === "http:" ? "ws:" : "wss:"}//${url.host}`;
	if (!origins.includes(origin)) origins.push(origin);
	return origins;
})();

// CSP for the renderer. The daemon is loopback-only, so network access is
// pinned to 127.0.0.1 (REST + SSE over http, terminal mux over ws), plus the
// cloud control-plane websocket origins above. The policy is injected here
// rather than written into index.html so the serve variant can differ: the
// same directives apply in dev, relaxed only where the dev server itself
// needs it. Enforcing CSP in dev keeps dev/packaged parity — a connect-src
// gap then fails on the developer's screen, not weeks later in a packaged
// build (that skew is exactly how the cloud-terminal block in #4666 shipped).
export function contentSecurityPolicy(mode: "build" | "serve"): string {
	return [
		"default-src 'self'",
		// react-refresh injects its inline preamble in serve mode; a hash is
		// impractical because the preamble changes with the plugin version.
		mode === "serve" ? "script-src 'self' 'unsafe-inline'" : "script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		// Repository avatars can come from self-hosted SCM instances whose origins
		// are only known at runtime. Keep the broad exception limited to images;
		// scripts, connections, frames, and other resource classes remain scoped.
		"img-src 'self' data: http://127.0.0.1:* http://*.localhost:* https:",
		"font-src 'self' data:",
		[
			"connect-src",
			"'self'",
			"http://127.0.0.1:*",
			"ws://127.0.0.1:*",
			// Session artifact files are read from the daemon's artifact preview
			// origin (ao-preview-artifact.<id>.localhost:<port>), a subdomain of
			// localhost that 'self' and 127.0.0.1 do not cover.
			"http://*.localhost:*",
			// Vite serves on localhost, which 'self' does not cover for the ws://
			// HMR socket.
			mode === "serve" ? "ws://localhost:*" : "",
			...POSTHOG_ORIGINS,
			...CLOUD_CP_WS_ORIGINS,
		]
			.filter(Boolean)
			.join(" "),
		"object-src 'none'",
		"base-uri 'self'",
		// Agent HTML renders (RenderFrame) are the only frames: sandboxed pages
		// served by the loopback daemon.
		// 'self' lets dev:web, whose API base is relative, frame a page; the page
		// keeps its own sandbox from the daemon's CSP header either way.
		// *.localhost is the inline-artifact origin an HTML artifact is framed from.
		"frame-src 'self' http://127.0.0.1:* http://*.localhost:*",
	].join("; ");
}

// Whether a saved address needs the per-host "Allow report pulls over plain
// HTTP" switch. Mirrors remotehost.NeedsInsecureOptIn in the daemon, which
// stays the authority on what it pulls; this only decides what Settings shows.
export function needsPlainHTTPOptIn(address: string): boolean {
	let parsed: URL;
	try {
		parsed = new URL(address.includes("://") ? address : `http://${address}`);
	} catch {
		return false;
	}
	if (parsed.protocol === "https:") return false;
	return !isLoopbackOrTailnet(parsed.hostname.toLowerCase());
}

function isLoopbackOrTailnet(host: string): boolean {
	if (host === "localhost" || host === "[::1]" || host.endsWith(".ts.net") || host.endsWith(".ts.net.")) return true;
	const ipv4 = /^(\d+)\.(\d+)\.\d+\.\d+$/.exec(host);
	if (ipv4) {
		const [first, second] = [Number(ipv4[1]), Number(ipv4[2])];
		// 127.0.0.0/8 is loopback; 100.64.0.0/10 is Tailscale's CGNAT range.
		return first === 127 || (first === 100 && second >= 64 && second <= 127);
	}
	// fd7a:115c:a1e0::/48 is Tailscale's IPv6 range.
	return host.startsWith("[fd7a:115c:a1e0:");
}

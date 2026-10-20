export const SSE_RECONNECT_BASE_MS = 1_000;
export const SSE_RECONNECT_CAP_MS = 30_000;
const FIRST_FRAME_TIMEOUT_MS = 15_000;

// Full jitter over the upper half of the capped exponential step, so many tabs
// reconnecting to one host after a blip do not retry in lockstep.
export function sseReconnectDelay(attempt: number, random: () => number = Math.random): number {
	const step = Math.min(SSE_RECONNECT_CAP_MS, SSE_RECONNECT_BASE_MS * 2 ** Math.max(0, attempt));
	return Math.round(step / 2 + (random() * step) / 2);
}

function resumeUrl(url: string, lastEventId: string): string {
	try {
		const next = new URL(url);
		if (!next.searchParams.has("after")) return url;
		next.searchParams.set("after", lastEventId);
		return next.toString();
	} catch {
		return url;
	}
}

// A frame must arrive before a stream is trusted. Failures reopen with backoff
// from the last event id; onFailure fires once per outage.
export function probeRemoteSse(
	url: string,
	eventNames: readonly string[],
	onEvent: (event: Event) => void,
	onVerified: () => void,
	onFailure: () => void,
): () => void {
	let source: EventSource | undefined;
	let closed = false;
	let verified = false;
	let down = false;
	let attempt = 0;
	let lastEventId = "";
	let timeout: ReturnType<typeof setTimeout> | undefined;
	let retry: ReturnType<typeof setTimeout> | undefined;
	const close = () => {
		closed = true;
		if (timeout) clearTimeout(timeout);
		if (retry) clearTimeout(retry);
		source?.close();
	};
	const open = () => {
		retry = undefined;
		verified = false;
		const current = new EventSource(lastEventId ? resumeUrl(url, lastEventId) : url);
		source = current;
		let stale = false;
		const fail = () => {
			if (closed || stale) return;
			stale = true;
			current.close();
			if (timeout) clearTimeout(timeout);
			if (!down) {
				down = true;
				onFailure();
			}
			if (closed) return;
			retry = setTimeout(open, sseReconnectDelay(attempt++));
		};
		const verify = () => {
			if (closed || stale || verified) return;
			verified = true;
			attempt = 0;
			if (timeout) clearTimeout(timeout);
			down = false;
			onVerified();
		};
		const delivered = (event: Event) => {
			if (closed || stale) return;
			const id = (event as MessageEvent).lastEventId;
			if (id) lastEventId = id;
			verify();
			onEvent(event);
		};
		current.onmessage = delivered;
		for (const name of eventNames) current.addEventListener(name, delivered);
		for (const name of ["ready", "cursor", "heartbeat"]) current.addEventListener(name, verify);
		current.onerror = fail;
		timeout = setTimeout(fail, FIRST_FRAME_TIMEOUT_MS);
	};
	try {
		if (typeof EventSource === "undefined") throw new Error("EventSource unavailable");
		open();
	} catch {
		down = true;
		onFailure();
	}
	return close;
}

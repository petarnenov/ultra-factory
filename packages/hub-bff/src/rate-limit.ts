import { isIP } from "node:net";
import type { IncomingMessage } from "node:http";

// clientKey identifies the caller for login throttling: the rightmost
// X-Forwarded-For value (ADR 0005), or the TCP peer when unparseable.
export function clientKey(req: IncomingMessage): string {
	const lines = req.headersDistinct["x-forwarded-for"];
	if (lines && lines.length > 0) {
		const entries = lines[lines.length - 1].split(",");
		const rightmost = entries[entries.length - 1].trim();
		if (isIP(rightmost)) return rightmost;
	}
	return req.socket.remoteAddress ?? "unknown";
}

// A fixed-window counter: at most `limit` attempts per key per window.
export class RateLimiter {
	private readonly windows = new Map<string, { start: number; count: number }>();
	private readonly limit: number;
	private readonly windowMs: number;
	private readonly now: () => number;

	constructor(limit: number, windowMs: number, now: () => number = Date.now) {
		this.limit = limit;
		this.windowMs = windowMs;
		this.now = now;
	}

	// Records one attempt. Returns 0 if allowed, otherwise the seconds until the
	// key may try again.
	hit(key: string): number {
		const now = this.now();
		if (this.windows.size > 10_000) {
			for (const [k, w] of this.windows) if (now - w.start >= this.windowMs) this.windows.delete(k);
		}
		const current = this.windows.get(key);
		if (!current || now - current.start >= this.windowMs) {
			this.windows.set(key, { start: now, count: 1 });
			return 0;
		}
		current.count += 1;
		if (current.count <= this.limit) return 0;
		return Math.max(1, Math.ceil((current.start + this.windowMs - now) / 1000));
	}
}

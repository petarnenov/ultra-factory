import { isIP } from "node:net";
import type { IncomingMessage } from "node:http";

// clientKey определя клиента за ограничаването на login по правилото от ADR 0005:
// най-десният запис в X-Forwarded-For във всички редове на header-а, т.е. този,
// който е добавил edge-ът на Railway. Всичко вляво от него се контролира от
// клиента. Ако най-десният запис не е валиден IP, се ползва TCP peer-ът, никога
// по-ранен запис. Ключът служи само за ограничаване и за нищо друго.
export function clientKey(req: IncomingMessage): string {
	const lines = req.headersDistinct["x-forwarded-for"];
	if (lines && lines.length > 0) {
		const entries = lines[lines.length - 1].split(",");
		const rightmost = entries[entries.length - 1].trim();
		if (isIP(rightmost)) return rightmost;
	}
	return req.socket.remoteAddress ?? "unknown";
}

// Брояч с фиксиран прозорец: най-много `limit` опита на ключ за прозорец.
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

	// Записва един опит. Връща 0, ако е позволен, иначе секундите, след които
	// ключът може да опита отново.
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

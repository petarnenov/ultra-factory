import { randomBytes } from "node:crypto";

// Sessions live in BFF memory only (ADR 0006): a redeploy logs everyone out.
// Each session has a sliding idle timeout and an absolute lifetime.
export const SESSION_IDLE_MS = 7 * 24 * 60 * 60 * 1000;
export const SESSION_ABSOLUTE_MS = 30 * 24 * 60 * 60 * 1000;

type Session = { createdAt: number; lastSeenAt: number };

export class SessionStore {
	private readonly sessions = new Map<string, Session>();
	private readonly now: () => number;

	constructor(now: () => number = Date.now) {
		this.now = now;
	}

	// Returns a random 256-bit session id.
	create(): string {
		this.sweep();
		const id = randomBytes(32).toString("base64url");
		const now = this.now();
		this.sessions.set(id, { createdAt: now, lastSeenAt: now });
		return id;
	}

	// Validates the id and, if it is live, slides its idle window.
	touch(id: string | undefined): boolean {
		if (!id) return false;
		const session = this.sessions.get(id);
		if (!session) return false;
		const now = this.now();
		if (now - session.lastSeenAt > SESSION_IDLE_MS || now - session.createdAt > SESSION_ABSOLUTE_MS) {
			this.sessions.delete(id);
			return false;
		}
		session.lastSeenAt = now;
		return true;
	}

	destroy(id: string | undefined): void {
		if (id) this.sessions.delete(id);
	}

	private sweep(): void {
		const now = this.now();
		for (const [id, s] of this.sessions) {
			if (now - s.lastSeenAt > SESSION_IDLE_MS || now - s.createdAt > SESSION_ABSOLUTE_MS) this.sessions.delete(id);
		}
	}
}

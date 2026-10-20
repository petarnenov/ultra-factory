import { afterEach, expect, it, vi } from "vitest";
import { SSE_RECONNECT_CAP_MS, probeRemoteSse, sseReconnectDelay } from "./remote-sse-probe";

class StubEventSource extends EventTarget {
	static instances: StubEventSource[] = [];
	onmessage: ((event: MessageEvent) => void) | null = null;
	onerror: (() => void) | null = null;
	closed = false;
	constructor(readonly url: string) {
		super();
		StubEventSource.instances.push(this);
	}
	close() { this.closed = true; }
	frame(name: string, lastEventId = "") { this.dispatchEvent(new MessageEvent(name, { data: "{}", lastEventId })); }
}

afterEach(() => {
	vi.useRealTimers();
	vi.unstubAllGlobals();
	StubEventSource.instances = [];
});

it("does not trust an opened but buffered stream; falls back once after the probe timeout", () => {
	vi.useFakeTimers();
	vi.stubGlobal("EventSource", StubEventSource);
	const verified = vi.fn();
	const failed = vi.fn();
	probeRemoteSse("https://host/events", ["changed"], vi.fn(), verified, failed);
	const source = StubEventSource.instances[0];
	expect(source.url).toBe("https://host/events");
	vi.advanceTimersByTime(15_000);
	expect(verified).not.toHaveBeenCalled();
	expect(failed).toHaveBeenCalledOnce();
	expect(source.closed).toBe(true);
	source.frame("ready");
	expect(verified).not.toHaveBeenCalled();
});

it("accepts a delivered frame and falls back if the verified stream fails", () => {
	vi.stubGlobal("EventSource", StubEventSource);
	const event = vi.fn();
	const verified = vi.fn();
	const failed = vi.fn();
	const close = probeRemoteSse("https://host/events", ["changed"], event, verified, failed);
	const source = StubEventSource.instances[0];
	source.frame("ready");
	source.frame("changed");
	expect(verified).toHaveBeenCalledOnce();
	expect(event).toHaveBeenCalledOnce();
	source.onerror?.();
	expect(failed).toHaveBeenCalledOnce();
	close();
	expect(source.closed).toBe(true);
});

it("reconnects after an error, resumes from the last event id and reports one failure per outage", () => {
	vi.useFakeTimers();
	vi.stubGlobal("EventSource", StubEventSource);
	const verified = vi.fn();
	const failed = vi.fn();
	probeRemoteSse("https://host/api/v1/events?after=latest", ["changed"], vi.fn(), verified, failed);
	StubEventSource.instances[0].frame("changed", "41");
	expect(verified).toHaveBeenCalledOnce();
	StubEventSource.instances[0].onerror?.();
	expect(failed).toHaveBeenCalledOnce();
	expect(StubEventSource.instances[0].closed).toBe(true);
	vi.advanceTimersByTime(1_000);
	expect(StubEventSource.instances).toHaveLength(2);
	expect(StubEventSource.instances[1].url).toBe("https://host/api/v1/events?after=41");
	StubEventSource.instances[1].onerror?.();
	expect(failed).toHaveBeenCalledOnce();
	vi.advanceTimersByTime(2_000);
	expect(StubEventSource.instances).toHaveLength(3);
	StubEventSource.instances[2].frame("ready");
	expect(verified).toHaveBeenCalledTimes(2);
	StubEventSource.instances[2].onerror?.();
	expect(failed).toHaveBeenCalledTimes(2);
});

it("stops retrying once closed", () => {
	vi.useFakeTimers();
	vi.stubGlobal("EventSource", StubEventSource);
	const close = probeRemoteSse("https://host/events", [], vi.fn(), vi.fn(), vi.fn());
	StubEventSource.instances[0].onerror?.();
	close();
	vi.advanceTimersByTime(10 * SSE_RECONNECT_CAP_MS);
	expect(StubEventSource.instances).toHaveLength(1);
});

it("backs off exponentially with jitter and never exceeds the cap", () => {
	expect(sseReconnectDelay(0, () => 0)).toBe(500);
	expect(sseReconnectDelay(0, () => 1)).toBe(1_000);
	expect(sseReconnectDelay(3, () => 1)).toBe(8_000);
	for (const attempt of [5, 10, 100]) {
		expect(sseReconnectDelay(attempt, () => 1)).toBe(SSE_RECONNECT_CAP_MS);
		expect(sseReconnectDelay(attempt, () => 0)).toBe(SSE_RECONNECT_CAP_MS / 2);
	}
});

import type { AoBridge } from "../../preload";

// The Electron preload bridge, read live. Undefined in the web build and the
// mock preview. Prefer aoBridge from ./bridge; use this only for the live read.
export function electronBridge(): AoBridge | undefined {
	return typeof window === "undefined" ? undefined : window.ao;
}

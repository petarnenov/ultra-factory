import type { AoBridge } from "../../preload";

// The Electron preload bridge, read live so callers see it as soon as it is
// installed (and tests can swap it). Undefined in the web build and the
// VITE_NO_ELECTRON mock preview. Prefer aoBridge from ./bridge, which falls back
// to browser stubs; use this only where the caller must know whether the
// desktop bridge exists. Outside this file and ./bridge, the renderer never
// touches window.ao directly (enforced by electron-bridge-boundary.test.ts).
export function electronBridge(): AoBridge | undefined {
	return typeof window === "undefined" ? undefined : window.ao;
}

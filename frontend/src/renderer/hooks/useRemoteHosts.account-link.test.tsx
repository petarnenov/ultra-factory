import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
	accountHosts: [] as { hostId: string; label: string; url: string; token: string }[],
	saveAccountRemoteHost: vi.fn(async () => undefined),
	remotes: {
		list: vi.fn(async () => []),
		importAccountHost: vi.fn(async () => undefined),
		pruneAccountHosts: vi.fn(async () => undefined),
		localAccountHost: vi.fn(),
		connect: vi.fn(),
		disconnect: vi.fn(async () => undefined),
	},
}));

vi.mock("../lib/bridge", () => ({ aoBridge: { remotes: mocks.remotes } }));
vi.mock("../lib/cloud-session", () => ({ useCloudSession: () => ({ status: "authenticated", session: { user: { id: "user-a" } } }) }));
vi.mock("./useSettings", () => ({ useSettings: () => ({ settings: { cloudControlPlaneUrl: "https://cloud.fixture.test" } }) }));
vi.mock("../lib/account-remote-hosts", () => ({
	listAccountRemoteHosts: async () => mocks.accountHosts,
	saveAccountRemoteHost: mocks.saveAccountRemoteHost,
}));

import { useUiStore } from "../stores/ui-store";
import { useRemoteHosts } from "./useRemoteHosts";

const self = { hostId: "h_air", label: "Petars-MacBook-Air", url: "http://100.126.82.17:3011", token: "b".repeat(64) };
const other = { hostId: "h_z600", label: "Z600", url: "http://100.106.144.10:3011", token: "c".repeat(64) };

beforeEach(() => {
	vi.clearAllMocks();
	mocks.accountHosts = [other];
	mocks.remotes.localAccountHost.mockResolvedValue(self);
	useUiStore.setState({ developerMode: true, remoteHosts: true });
});

afterEach(() => {
	act(() => useUiStore.setState({ developerMode: false, remoteHosts: false }));
});

it("adds this machine to the account when it is missing", async () => {
	renderHook(() => useRemoteHosts());
	await waitFor(() => expect(mocks.saveAccountRemoteHost).toHaveBeenCalledWith("https://cloud.fixture.test", self));
	expect(mocks.remotes.localAccountHost).toHaveBeenCalledWith("user-a");
	await waitFor(() => expect(mocks.remotes.importAccountHost).toHaveBeenCalledWith("user-a", { ...other, password: other.token }));
});

it("moves this machine's stale account address to its tailnet URL and keeps the account label", async () => {
	mocks.accountHosts = [other, { ...self, label: "Air", url: "http://192.168.0.20:3011" }];
	renderHook(() => useRemoteHosts());
	await waitFor(() => expect(mocks.saveAccountRemoteHost).toHaveBeenCalledWith("https://cloud.fixture.test", { ...self, label: "Air" }));
});

it("does not rewrite an up-to-date account entry", async () => {
	mocks.accountHosts = [other, { ...self, label: "Air" }];
	renderHook(() => useRemoteHosts());
	await waitFor(() => expect(mocks.remotes.pruneAccountHosts).toHaveBeenCalled());
	expect(mocks.saveAccountRemoteHost).not.toHaveBeenCalled();
});

it.each([
	["has nothing to link", () => mocks.remotes.localAccountHost.mockResolvedValue(null)],
	["fails to link", () => mocks.remotes.localAccountHost.mockRejectedValue(new Error("daemon down"))],
])("still syncs the other hosts when this machine %s", async (_case, arrange) => {
	arrange();
	renderHook(() => useRemoteHosts());
	await waitFor(() => expect(mocks.remotes.pruneAccountHosts).toHaveBeenCalledWith("user-a", ["h_z600"]));
	expect(mocks.remotes.importAccountHost).toHaveBeenCalledWith("user-a", { ...other, password: other.token });
	expect(mocks.saveAccountRemoteHost).not.toHaveBeenCalled();
});

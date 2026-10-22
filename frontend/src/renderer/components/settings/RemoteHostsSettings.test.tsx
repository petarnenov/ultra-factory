import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { useUiStore } from "../../stores/ui-store";

const saved = vi.hoisted(() => ({ entries: [] as Array<{ hostId: string; label: string; url: string; tailscale?: boolean; online?: boolean; allowInsecureHTTP?: boolean }> }));
const tailnet = vi.hoisted(() => ({ status: { available: true, hidden: [] } as { available: boolean; error?: string; hidden: Array<{ hostId: string; label: string; name: string; address: string; url: string; online: boolean }> } }));
const account = vi.hoisted(() => ({ status: "authenticated" as "authenticated" | "unauthenticated" }));
const cloudSettings = vi.hoisted(() => ({ url: "" }));
const remotes = vi.hoisted(() => ({
	list: vi.fn(async () => saved.entries),
	add: vi.fn(async ({ label, url }: { label: string; url: string }) => {
		saved.entries = [{ hostId: "box-a", label, url }];
		return "online" as "online" | "incompatible";
	}),
	connect: vi.fn(async (url: string) => ({ hostId: "box-a", label: "Box A", url, base: "http://127.0.0.1:4000" })),
	disconnect: vi.fn(async () => undefined),
	remove: vi.fn(async () => undefined),
	setAllowInsecureHTTP: vi.fn(async () => undefined),
	issueAccountToken: vi.fn(async () => ({ token: "token", url: "https://host.example" })),
	update: vi.fn(async (_oldUrl: string, changes: { label?: string; url?: string }) => {
		saved.entries = saved.entries.map((host) => ({ ...host, label: changes.label ?? host.label, url: changes.url ?? host.url }));
		return "online" as const;
	}),
	tailnet: vi.fn(async () => tailnet.status),
	hide: vi.fn(async (hostId: string) => { saved.entries = saved.entries.filter((host) => host.hostId !== hostId); }),
	unhide: vi.fn(async () => undefined),
}));
vi.mock("../../lib/bridge", () => ({ aoBridge: { remotes } }));
vi.mock("../../lib/cloud-session", () => ({ useCloudSession: () => ({ status: account.status, signIn: vi.fn() }) }));
vi.mock("../../hooks/useCloudLocalAuth", () => ({ useCloudLocalAuth: () => ({ available: false }) }));
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ settings: { cloudControlPlaneUrl: cloudSettings.url } }) }));
const platform = vi.hoisted(() => ({ web: false }));
vi.mock("../../lib/platform", () => ({ get isWebBuild() { return platform.web; } }));

import { RemoteHostsSettings } from "./RemoteHostsSettings";

const renderSettings = () => render(<QueryClientProvider client={new QueryClient()}><RemoteHostsSettings /></QueryClientProvider>);

afterEach(() => {
	account.status = "authenticated";
	cloudSettings.url = "";
	platform.web = false;
	saved.entries = [];
	tailnet.status = { available: true, hidden: [] };
	useUiStore.setState({ remoteHosts: false, remoteHostsCloudSync: false });
	remotes.hide.mockClear();
	remotes.unhide.mockClear();
	remotes.connect.mockClear();
	remotes.list.mockClear();
	remotes.add.mockClear();
	remotes.update.mockClear();
	remotes.remove.mockClear();
	remotes.issueAccountToken.mockClear();
});

it("lists hosts without an AO Cloud sign-in and keeps cloud sync out of sight", async () => {
	account.status = "unauthenticated";
	saved.entries = [{ hostId: "box-a", label: "Box A", url: "http://box-a:3001" }];
	renderSettings();
	expect(await screen.findByRole("button", { name: "Edit Box A" })).toBeVisible();
	expect(screen.queryByRole("button", { name: "Sign in to AO Cloud" })).toBeNull();
	expect(screen.queryByRole("switch", { name: "Sync hosts through AO Cloud" })).toBeNull();
});

it("offers cloud sync only to a signed-in account, off by default", async () => {
	renderSettings();
	const sync = await screen.findByRole("switch", { name: "Sync hosts through AO Cloud" });
	expect(sync).toHaveAttribute("aria-checked", "false");
	fireEvent.click(sync);
	expect(useUiStore.getState().remoteHostsCloudSync).toBe(true);
});

it("shows Tailscale hosts with their state and hides one through the daemon", async () => {
	saved.entries = [
		{ hostId: "h_z600", label: "hpz600", url: "http://100.106.144.10:3011", tailscale: true, online: true },
		{ hostId: "h_air", label: "petars-macbook-air", url: "http://100.126.82.17:3011", tailscale: true, online: false },
	];
	tailnet.status = { available: true, hidden: [{ hostId: "h_imac", label: "petars-imac", name: "petars-imac", address: "100.100.160.17", url: "http://100.100.160.17:3011", online: true }] };
	renderSettings();
	expect(await screen.findByText("hpz600")).toBeVisible();
	expect(screen.getByText("Online")).toBeVisible();
	expect(screen.getByText("Offline")).toBeVisible();
	expect(screen.getByText("http://100.106.144.10:3011 · Tailscale identity, no password")).toBeVisible();
	expect(screen.queryByRole("button", { name: "Remove hpz600" })).toBeNull();
	fireEvent.click(screen.getByRole("button", { name: "Show petars-imac" }));
	await waitFor(() => expect(remotes.unhide).toHaveBeenCalledWith("h_imac"));
	fireEvent.click(screen.getByRole("button", { name: "Hide hpz600" }));
	await waitFor(() => expect(remotes.hide).toHaveBeenCalledWith("h_z600"));
	await waitFor(() => expect(screen.queryByText("hpz600")).toBeNull());
});

it("renames a Tailscale host without asking for an address or password", async () => {
	saved.entries = [{ hostId: "h_z600", label: "hpz600", url: "http://100.106.144.10:3011", tailscale: true, online: true }];
	renderSettings();
	fireEvent.click(await screen.findByRole("button", { name: "Rename hpz600" }));
	expect(screen.queryByLabelText("Connection password")).toBeNull();
	expect(screen.queryByRole("textbox", { name: "Address" })).toBeNull();
	fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Z600" } });
	fireEvent.click(screen.getByRole("button", { name: "Save host" }));
	await waitFor(() => expect(remotes.update).toHaveBeenCalledWith("http://100.106.144.10:3011", { label: "Z600" }));
});

it("renames a Tailscale host with cloud sync on without trying account linking", async () => {
	cloudSettings.url = "https://api.aoagents.dev";
	useUiStore.setState({ remoteHostsCloudSync: true });
	saved.entries = [{ hostId: "h_z600", label: "hpz600", url: "http://100.106.144.10:3011", tailscale: true, online: true }];
	renderSettings();
	fireEvent.click(await screen.findByRole("button", { name: "Rename hpz600" }));
	fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Z600" } });
	fireEvent.click(screen.getByRole("button", { name: "Save host" }));
	await waitFor(() => expect(remotes.update).toHaveBeenCalledWith("http://100.106.144.10:3011", { label: "Z600" }));
	await waitFor(() => expect(screen.getByRole("textbox", { name: "Name" })).toHaveValue(""));
	expect(remotes.issueAccountToken).not.toHaveBeenCalled();
});

it("says when Tailscale is unavailable instead of hiding the section", async () => {
	tailnet.status = { available: false, error: "tailscale is unavailable: backend state Stopped", hidden: [] };
	renderSettings();
	expect(await screen.findByText("Tailscale unavailable")).toBeVisible();
	expect(screen.getByText("tailscale is unavailable: backend state Stopped")).toBeVisible();
});

it("shows the plain-http switch with its helper text and blocked state only for plain-http hosts", async () => {
	useUiStore.setState({ remoteHosts: true });
	saved.entries = [
		{ hostId: "box-lan", label: "Office", url: "http://192.168.0.164:3011" },
		{ hostId: "box-https", label: "Cloud", url: "https://build.example" },
		{ hostId: "box-loop", label: "Tunnel", url: "http://127.0.0.1:3011" },
		{ hostId: "box-cgnat", label: "Tailnet IP", url: "http://100.106.144.10:3011" },
		{ hostId: "box-ts", label: "Tailnet name", url: "http://z600.tail1234.ts.net:3011" },
	];
	renderSettings();
	const switches = await screen.findAllByRole("switch", { name: "Allow report pulls over plain HTTP" });
	expect(switches).toHaveLength(1);
	expect(screen.getByText("AO will send this host's saved password unattended over an unencrypted connection. Turn this on only on a network you trust.")).toBeVisible();
	expect(screen.getByText("blocked (plain http)")).toBeVisible();
	expect(switches[0]).toHaveAttribute("aria-checked", "false");
});

it("writes the per-host flag through the flag-only path when the switch is turned on", async () => {
	useUiStore.setState({ remoteHosts: true });
	saved.entries = [{ hostId: "box-lan", label: "Office", url: "http://192.168.0.164:3011" }];
	renderSettings();
	fireEvent.click(await screen.findByRole("switch", { name: "Allow report pulls over plain HTTP" }));
	await waitFor(() => expect(remotes.setAllowInsecureHTTP).toHaveBeenCalledWith("http://192.168.0.164:3011", true));
	expect(remotes.update).not.toHaveBeenCalled();
});

it("turns the switch off without probing, so an offline or unauthorized host cannot keep the opt-in", async () => {
	useUiStore.setState({ remoteHosts: true });
	saved.entries = [{ hostId: "box-lan", label: "Office", url: "http://192.168.0.164:3011", allowInsecureHTTP: true }];
	remotes.setAllowInsecureHTTP.mockImplementationOnce(async () => undefined);
	renderSettings();
	fireEvent.click(await screen.findByRole("switch", { name: "Allow report pulls over plain HTTP" }));
	await waitFor(() => expect(remotes.setAllowInsecureHTTP).toHaveBeenCalledWith("http://192.168.0.164:3011", false));
	expect(remotes.update).not.toHaveBeenCalled();
	expect(screen.queryByRole("alert")).toBeNull();
});

it("shows the error when the flag cannot be written", async () => {
	useUiStore.setState({ remoteHosts: true });
	saved.entries = [{ hostId: "box-lan", label: "Office", url: "http://192.168.0.164:3011" }];
	remotes.setAllowInsecureHTTP.mockImplementationOnce(async () => { throw new Error("Host does not belong to this account."); });
	renderSettings();
	fireEvent.click(await screen.findByRole("switch", { name: "Allow report pulls over plain HTTP" }));
	expect(await screen.findByRole("alert")).toHaveTextContent("Host does not belong to this account.");
});

it("does not connect a newly paired host after Remote hosts is turned off", async () => {
	useUiStore.setState({ remoteHosts: true });
	let finishAdd: ((health: "online") => void) | undefined;
	remotes.add.mockImplementationOnce(() => new Promise((resolve) => { finishAdd = resolve; }));
	renderSettings();
	fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Box A" } });
	fireEvent.change(screen.getByRole("textbox", { name: "Address" }), { target: { value: "http://box-a:3001" } });
	fireEvent.change(screen.getByLabelText("Connection password"), { target: { value: "secret123" } });
	const submit = screen.getByRole("button", { name: "Add host" });
	fireEvent.click(submit);
	expect(submit).toBeDisabled();
	act(() => useUiStore.setState({ remoteHosts: false }));
	await act(async () => finishAdd?.("online"));
	await waitFor(() => expect(screen.getByRole("textbox", { name: "Name" })).toHaveValue(""));
	expect(remotes.connect).not.toHaveBeenCalled();
});

it("pairs a host and shows it in the saved-host list", async () => {
	renderSettings();
	expect(screen.getByRole("switch", { name: "Connect to remote hosts" })).toHaveAttribute("aria-checked", "false");
	fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Box A" } });
	fireEvent.change(screen.getByRole("textbox", { name: "Address" }), { target: { value: "http://box-a:3001" } });
	fireEvent.change(screen.getByLabelText("Connection password"), { target: { value: "secret123" } });
	fireEvent.click(screen.getByRole("button", { name: "Add host" }));
	await waitFor(() => expect(screen.getByText("Box A")).toBeVisible());
	expect(screen.getByRole("switch", { name: "Connect to remote hosts" })).toHaveAttribute("aria-checked", "true");
	expect(remotes.add).toHaveBeenCalledWith({ label: "Box A", url: "http://box-a:3001", password: "secret123" });
	expect(screen.getByLabelText("Connection password")).toHaveAttribute("type", "password");
});

it("explains an incompatible host without adding it", async () => {
	remotes.add.mockResolvedValueOnce("incompatible");
	renderSettings();
	fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Box A" } });
	fireEvent.change(screen.getByRole("textbox", { name: "Address" }), { target: { value: "http://box-a:3001" } });
	fireEvent.change(screen.getByLabelText("Connection password"), { target: { value: "secret123" } });
	fireEvent.click(screen.getByRole("button", { name: "Add host" }));
	await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Update AO"));
	expect(saved.entries).toEqual([]);
});

it("explains how to re-pair a saved host without an identity", async () => {
	saved.entries = [{ hostId: "", label: "Old Box", url: "http://old-box:3001" }];
	renderSettings();
	await screen.findByText(/Old Box/);
	expect(screen.getByText(/edit this host and enter its connection password/i)).toBeVisible();
	fireEvent.click(screen.getByRole("button", { name: "Edit Old Box" }));
	expect(screen.getByLabelText("Connection password")).toBeRequired();
	fireEvent.change(screen.getByLabelText("Connection password"), { target: { value: "new-secret" } });
	fireEvent.click(screen.getByRole("button", { name: "Save host" }));
	await waitFor(() => expect(remotes.add).toHaveBeenCalledWith({
		label: "Old Box",
		url: "http://old-box:3001",
		password: "new-secret",
	}));
	expect(remotes.update).not.toHaveBeenCalled();
});

it("edits a saved host without reading or replacing its password", async () => {
	saved.entries = [{ hostId: "box-a", label: "Box A", url: "http://box-a:3001" }];
	renderSettings();
	await screen.findByRole("button", { name: "Edit Box A" });
	fireEvent.click(screen.getByRole("button", { name: "Edit Box A" }));
	expect(screen.getByLabelText("Connection password")).toHaveValue("");
	expect(screen.getByLabelText("Connection password")).not.toBeRequired();
	fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "Office" } });
	fireEvent.change(screen.getByRole("textbox", { name: "Address" }), { target: { value: "http://office:3011" } });
	fireEvent.click(screen.getByRole("button", { name: "Save host" }));
	await waitFor(() => expect(remotes.update).toHaveBeenCalledWith("http://box-a:3001", {
		label: "Office",
		url: "http://office:3011",
	}));
	expect(await screen.findByText("Office")).toBeVisible();
});

it("requires confirmation before forgetting a host", async () => {
	saved.entries = [{ hostId: "box-a", label: "Box A", url: "http://box-a:3001" }];
	remotes.remove.mockImplementationOnce(async () => { saved.entries = []; });
	renderSettings();
	fireEvent.click(await screen.findByRole("button", { name: "Remove Box A" }));
	expect(remotes.remove).not.toHaveBeenCalled();
	expect(screen.getByText(/Sessions keep running on that host/i)).toBeVisible();
	fireEvent.click(screen.getByRole("button", { name: "Remove" }));
	await waitFor(() => expect(remotes.remove).toHaveBeenCalledWith("http://box-a:3001"));
	expect(await screen.findByText("No hosts yet. Add one below.")).toBeVisible();
});

it("lists the hub's hosts on the web without AO Cloud sign-in, edit, or tunnel setup", async () => {
	platform.web = true;
	account.status = "unauthenticated";
	saved.entries = [{ hostId: "h_worker", label: "Worker 1", url: "http://worker-1.railway.internal:3011" }];
	renderSettings();
	await waitFor(() => expect(screen.getByText("Worker 1")).toBeVisible());
	expect(screen.queryByRole("button", { name: "Sign in to AO Cloud" })).toBeNull();
	expect(screen.queryByRole("button", { name: "Edit Worker 1" })).toBeNull();
	expect(screen.getByRole("button", { name: "Remove Worker 1" })).toBeVisible();
	expect(screen.queryByText(/--tunnel/)).toBeNull();
	expect(screen.queryByText(/Cloudflare/)).toBeNull();
	expect(screen.getByRole("textbox", { name: "Address" })).toBeVisible();
	expect(screen.getByLabelText("Connection password")).toBeVisible();
});

it("hides the Remote hosts switch on the web, where the hub's hosts are always connected", async () => {
	platform.web = true;
	saved.entries = [{ hostId: "h_worker", label: "Worker 1", url: "http://worker-1.railway.internal:3011" }];
	renderSettings();
	await waitFor(() => expect(screen.getByText("Worker 1")).toBeVisible());
	expect(screen.queryByRole("switch", { name: "Connect to remote hosts" })).toBeNull();
});

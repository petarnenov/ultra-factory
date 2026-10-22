import { useCallback, useEffect, useRef, useState } from "react";
import { aoBridge } from "../lib/bridge";
import { connectHost, connectedHosts, disconnectHost } from "../lib/host-clients";
import { useCloudSession } from "../lib/cloud-session";
import { listAccountRemoteHosts } from "../lib/account-remote-hosts";
import { isWebBuild } from "../lib/platform";
import { useSettings } from "./useSettings";
import { useUiStore } from "../stores/ui-store";

const HOSTS_CHANGED_EVENT = "ao:remote-hosts-changed";
const OFFLINE_RETRY_MS = 15_000;
// Re-read tailnet discovery and enabled account sync on this cadence.
const DISCOVERY_REFRESH_MS = 30_000;
const NO_HOSTS: RemoteHost[] = [];

function isOfflineError(error: unknown): boolean {
	return error instanceof Error && error.message.endsWith(" is offline");
}

export function requestRemoteHostsRefresh(): void {
	window.dispatchEvent(new Event(HOSTS_CHANGED_EVENT));
}

export type RemoteHost = {
	hostId: string;
	label: string;
	url: string;
	status: "connecting" | "connected" | "offline";
	failureReason?: "unauthorized" | "incompatible";
	allowInsecureHTTP?: boolean;
	// Tailscale hosts are discovered peers; online mirrors peer state.
	tailscale?: boolean;
	online?: boolean;
};

// Soft refresh rereads discovery but skips saved hosts with rejected passwords.
// This avoids stale credentials repeatedly hitting host lockouts.
type RefreshOptions = { soft?: boolean };

export function useRemoteHosts(): { hosts: RemoteHost[]; refresh: (options?: RefreshOptions) => Promise<void> } {
	const remoteHostsEnabled = useUiStore((state) => isWebBuild || (state.developerMode && state.remoteHosts));
	const cloudSync = useUiStore((state) => state.remoteHostsCloudSync);
	const { status, session } = useCloudSession();
	// Tailscale discovery needs no account; the AO Cloud account matters only
	// with cloud sync on. The web build's hosts come from the hub BFF instead.
	const accountId = cloudSync && status === "authenticated" ? session?.user.id ?? "" : "";
	const { settings } = useSettings();
	const cloudBaseUrl = isWebBuild || !cloudSync || !accountId ? "" : settings?.cloudControlPlaneUrl ?? "";
	const enabled = remoteHostsEnabled;
	const enabledRef = useRef(enabled);
	enabledRef.current = enabled;
	const accountRef = useRef(accountId);
	accountRef.current = accountId;
	const accountLoading = !isWebBuild && status === "loading";
	const previousAccount = useRef<string | undefined>(accountLoading ? undefined : accountId);
	const [hosts, setHosts] = useState<RemoteHost[]>([]);
	const hostsRef = useRef(hosts);
	hostsRef.current = hosts;
	const retryableHosts = useRef(new Set<string>());
	const retryingHosts = useRef(new Set<string>());
	const savedHostUrls = useRef(new Map<string, string>());
	const refreshGeneration = useRef(0);
	const refresh = useCallback(async ({ soft = false }: RefreshOptions = {}) => {
		if (!enabledRef.current) return;
		const generation = ++refreshGeneration.current;
		const current = () => enabledRef.current && accountRef.current === accountId && refreshGeneration.current === generation;
		let saved = await aoBridge.remotes.list();
		if (!current()) return;
		setHosts((previous) => saved.map((host) => ({
			...host,
			status: previous.some((entry) => entry.hostId === host.hostId && entry.url === host.url && entry.status === "connected") ? "connected" : "connecting",
		})));
		if (cloudBaseUrl) {
			try {
				const accountHosts = await listAccountRemoteHosts(cloudBaseUrl);
				if (!current()) return;
				for (const host of accountHosts) await aoBridge.remotes.importAccountHost(accountId, { ...host, password: host.token });
				await aoBridge.remotes.pruneAccountHosts(accountId, accountHosts.map((host) => host.hostId));
				saved = await aoBridge.remotes.list();
			} catch { /* Cloud outage must not hide locally paired machines. */ }
		}
		if (!current()) return;
		await Promise.all(connectedHosts().filter((hostId) => !saved.some((host) => host.hostId === hostId)).map(disconnectHost));
		if (!current()) return;
		savedHostUrls.current = new Map(saved.map((host) => [host.hostId, host.url]));
		const rejected = (host: RemoteHost) => host.status === "offline" && host.failureReason === "unauthorized";
		const skipped = new Set(saved
			.filter((host) => soft && !host.tailscale && hostsRef.current.some((entry) => entry.hostId === host.hostId && entry.url === host.url && rejected(entry)))
			.map((host) => host.hostId));
		setHosts((previous) => saved.map((host) => {
			const known = previous.find((entry) => entry.hostId === host.hostId && entry.url === host.url);
			if (known && skipped.has(host.hostId)) return { ...known, ...host, status: "offline", failureReason: "unauthorized" };
			return { ...host, status: known?.status === "connected" ? "connected" : "connecting" };
		}));
		await Promise.all(saved.filter((savedHost) => !skipped.has(savedHost.hostId)).map(async (savedHost) => {
			let status: RemoteHost["status"] = "connected";
			let failureReason: RemoteHost["failureReason"];
			let connectedHostId = savedHost.hostId;
			try {
				const shouldAdopt = () => current() && savedHostUrls.current.get(savedHost.hostId) === savedHost.url;
				connectedHostId = (await connectHost(savedHost.url, savedHost.hostId, shouldAdopt)).hostId;
				if (!shouldAdopt()) {
					if (savedHostUrls.current.get(savedHost.hostId) !== savedHost.url) await aoBridge.remotes.disconnect(savedHost.url);
					return;
				}
				retryableHosts.current.delete(savedHost.hostId);
			} catch (error) {
				status = "offline";
				if (error instanceof Error && error.message.endsWith(" is unauthorized")) failureReason = "unauthorized";
				if (error instanceof Error && error.message.endsWith(" is incompatible")) failureReason = "incompatible";
				if (!current()) return;
				if (isOfflineError(error)) retryableHosts.current.add(savedHost.hostId);
				else retryableHosts.current.delete(savedHost.hostId);
				// A failed reconnect must not leave the old proxy marked connected.
				try { await disconnectHost(savedHost.hostId); } catch { /* Keep the offline state visible. */ }
			}
			if (!current()) return;
			setHosts((current) => current.map((host) => host.url === savedHost.url && host.hostId === savedHost.hostId ? { ...host, hostId: connectedHostId, status, ...(failureReason ? { failureReason } : {}) } : host));
		}));
	}, [cloudBaseUrl, accountId]);

	useEffect(() => {
		// Mounting another observer must not retire the shell's live proxies.
		if (accountLoading && remoteHostsEnabled) return;
		if (!accountLoading && previousAccount.current !== undefined && previousAccount.current !== accountId) {
			refreshGeneration.current++;
			setHosts([]);
			for (const hostId of connectedHosts()) void disconnectHost(hostId);
		}
		if (!accountLoading) previousAccount.current = accountId;
		if (enabled) {
			void refresh();
			return;
		}
		refreshGeneration.current++;
		setHosts([]);
		retryableHosts.current.clear();
		savedHostUrls.current.clear();
		for (const hostId of connectedHosts()) void disconnectHost(hostId);
	}, [enabled, refresh, accountLoading, remoteHostsEnabled]);
	useEffect(() => {
		if (!enabled) return;
		const timer = setInterval(() => {
			for (const host of hostsRef.current) {
				if (host.status !== "offline" || !retryableHosts.current.has(host.hostId) || retryingHosts.current.has(host.hostId)) continue;
				retryingHosts.current.add(host.hostId);
				const generation = refreshGeneration.current;
				void connectHost(host.url, host.hostId, () => enabledRef.current && generation === refreshGeneration.current && savedHostUrls.current.get(host.hostId) === host.url).then(async () => {
					if (!enabledRef.current) {
						await aoBridge.remotes.disconnect(host.url);
						return;
					}
					if (generation !== refreshGeneration.current) {
						if (savedHostUrls.current.get(host.hostId) !== host.url) await aoBridge.remotes.disconnect(host.url);
						return;
					}
					retryableHosts.current.delete(host.hostId);
					setHosts((current) => current.map((entry) => entry.hostId === host.hostId && entry.url === host.url
						? { ...entry, status: "connected" } : entry));
				}).catch((error: unknown) => {
					if (!isOfflineError(error)) retryableHosts.current.delete(host.hostId);
				}).finally(() => retryingHosts.current.delete(host.hostId));
			}
		}, OFFLINE_RETRY_MS);
		return () => clearInterval(timer);
	}, [enabled]);
	useEffect(() => {
		if (!enabled) return;
		const onChanged = () => { void refresh(); };
		window.addEventListener(HOSTS_CHANGED_EVENT, onChanged);
		return () => window.removeEventListener(HOSTS_CHANGED_EVENT, onChanged);
	}, [enabled, refresh]);
	useEffect(() => {
		if (!enabled || isWebBuild) return;
		const timer = setInterval(() => { if (!document.hidden) void refresh({ soft: true }); }, DISCOVERY_REFRESH_MS);
		return () => clearInterval(timer);
	}, [enabled, refresh]);

	return { hosts: enabled ? hosts : NO_HOSTS, refresh };
}

import { Pencil, Server, Trash2 } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { aoBridge } from "../../lib/bridge";
import { disconnectHost } from "../../lib/host-clients";
import { requestRemoteHostsRefresh } from "../../hooks/useRemoteHosts";
import { needsPlainHTTPOptIn } from "../../lib/remote-reports-transport";
import { useCloudSession } from "../../lib/cloud-session";
import { deleteAccountRemoteHost, saveAccountRemoteHost } from "../../lib/account-remote-hosts";
import { isWebBuild } from "../../lib/platform";
import { useSettings } from "../../hooks/useSettings";
import { useUiStore } from "../../stores/ui-store";
import { cn } from "../../lib/utils";
import { ConfirmDialog } from "../ConfirmDialog";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Switch } from "../ui/switch";
import { SettingsRow } from "./SettingsRow";
import { SettingsSection } from "./SettingsSection";

type TailnetStatus = Awaited<ReturnType<typeof aoBridge.remotes.tailnet>>;
type SavedHost = { hostId: string; label: string; url: string; allowInsecureHTTP?: boolean; tailscale?: boolean; online?: boolean };

export function RemoteHostsSettings({ titleHidden }: { titleHidden?: boolean }) {
	const { t } = useTranslation();
	const enabled = useUiStore((state) => state.remoteHosts);
	const setEnabled = useUiStore((state) => state.setRemoteHosts);
	const cloudSync = useUiStore((state) => state.remoteHostsCloudSync);
	const setCloudSync = useUiStore((state) => state.setRemoteHostsCloudSync);
	const { status } = useCloudSession();
	const { settings } = useSettings();
	// Cloud sync is an explicit opt-in on top of Tailscale discovery. The web
	// build signs in to the hub BFF, which owns the registry, and syncs nothing.
	const signedIn = !isWebBuild && status === "authenticated";
	const cloudBaseUrl = signedIn && cloudSync ? settings?.cloudControlPlaneUrl ?? "" : "";
	const [saved, setSaved] = useState<SavedHost[]>([]);
	const [tailnet, setTailnet] = useState<TailnetStatus | null>(null);
	const [loading, setLoading] = useState(true);
	const [loadError, setLoadError] = useState("");
	const [label, setLabel] = useState("");
	const [url, setUrl] = useState("");
	const [password, setPassword] = useState("");
	const [editing, setEditing] = useState<SavedHost | null>(null);
	const [busy, setBusy] = useState(false);
	const [formError, setFormError] = useState("");
	const [confirmingRemoval, setConfirmingRemoval] = useState<SavedHost | null>(null);
	const [removeBusy, setRemoveBusy] = useState(false);
	const [removeError, setRemoveError] = useState("");
	const [hideError, setHideError] = useState("");
	const [reportsError, setReportsError] = useState("");
	const formRef = useRef<HTMLFormElement>(null);

	const load = async () => {
		setLoading(true);
		setLoadError("");
		try {
			setSaved(await aoBridge.remotes.list());
			setTailnet(isWebBuild ? null : await aoBridge.remotes.tailnet());
		} catch (cause) {
			setLoadError(cause instanceof Error ? cause.message : t("remote.loadHostsFailed"));
		} finally {
			setLoading(false);
		}
	};

	useEffect(() => {
		void load();
		// The bridge is fixed for the lifetime of this Settings page.
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);

	const resetForm = () => {
		setEditing(null);
		setLabel("");
		setUrl("");
		setPassword("");
		setFormError("");
	};

	const startEdit = (host: SavedHost) => {
		setEditing(host);
		setLabel(host.label);
		setUrl(host.url);
		setPassword("");
		setFormError("");
		formRef.current?.scrollIntoView?.({ block: "nearest" });
	};

	const save = async (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		const nextLabel = label.trim();
		const nextUrl = editing?.tailscale ? editing.url : url.trim();
		if (!nextLabel || !nextUrl || (!editing?.hostId && !password)) return;
		setBusy(true);
		setFormError("");
		try {
			// Old saved records have no identity, so re-pair them through add.
			const health = editing?.tailscale
				? await aoBridge.remotes.update(editing.url, { label: nextLabel })
				: editing?.hostId
					? await aoBridge.remotes.update(editing.url, {
						label: nextLabel,
						url: nextUrl,
						...(password ? { password } : {}),
					})
					: await aoBridge.remotes.add({ label: nextLabel, url: nextUrl, password });
			if (health === "offline") throw new Error(t("remote.hostOffline"));
			if (health === "unauthorized") throw new Error(t("remote.hostUnauthorized"));
			if (health === "not-a-daemon") throw new Error(t("remote.hostNotDaemon"));
			if (health === "incompatible") throw new Error(t("remote.hostIncompatible"));
			if (editing?.hostId && !editing.tailscale) await disconnectHost(editing.hostId);
			if (editing && !editing.hostId && editing.url !== nextUrl) await aoBridge.remotes.remove(editing.url);
			const current = (await aoBridge.remotes.list()).find((item) => item.url === nextUrl);
			if (current?.hostId && cloudBaseUrl) {
				const { token, url: accountUrl } = await aoBridge.remotes.issueAccountToken(current.url);
				await saveAccountRemoteHost(cloudBaseUrl, { hostId: current.hostId, label: current.label, url: accountUrl, token });
			}
			await load();
			resetForm();
			if (!enabled) setEnabled(true);
			requestRemoteHostsRefresh();
		} catch (cause) {
			setFormError(cause instanceof Error ? cause.message : t("remote.addHostFailed"));
		} finally {
			setBusy(false);
		}
	};

	const setPlainHTTPAllowed = async (host: SavedHost, allowed: boolean) => {
		setBusy(true);
		setReportsError("");
		try {
			// No probe and no reconnect: turning the switch off must work even when
			// the host is offline or its password has changed.
			await aoBridge.remotes.setAllowInsecureHTTP(host.url, allowed);
			await load();
			requestRemoteHostsRefresh();
		} catch (cause) {
			setReportsError(cause instanceof Error ? cause.message : t("remote.addHostFailed"));
		} finally {
			setBusy(false);
		}
	};

	const remove = async () => {
		if (!confirmingRemoval) return;
		setRemoveBusy(true);
		setRemoveError("");
		try {
			if (confirmingRemoval.hostId && cloudBaseUrl) await deleteAccountRemoteHost(cloudBaseUrl, confirmingRemoval.hostId);
			await aoBridge.remotes.remove(confirmingRemoval.url);
			if (confirmingRemoval.hostId) await disconnectHost(confirmingRemoval.hostId);
			await load();
			setConfirmingRemoval(null);
			requestRemoteHostsRefresh();
		} catch (cause) {
			setRemoveError(cause instanceof Error ? cause.message : t("remote.removeHostFailed"));
		} finally {
			setRemoveBusy(false);
		}
	};

	const setHidden = async (host: { hostId: string }, hidden: boolean) => {
		setHideError("");
		try {
			if (hidden) {
				await aoBridge.remotes.hide(host.hostId);
				await disconnectHost(host.hostId);
			} else {
				await aoBridge.remotes.unhide(host.hostId);
			}
			await load();
			requestRemoteHostsRefresh();
		} catch (cause) {
			setHideError(cause instanceof Error ? cause.message : t("remote.hideHostFailed"));
		}
	};

	const tailscaleHosts = saved.filter((host) => host.tailscale);
	const pairedHosts = saved.filter((host) => !host.tailscale);
	const hiddenHosts = tailnet?.hidden ?? [];

	return <>
		{/* The web build connects the hub's hosts; the switch has nothing to do. */}
		{isWebBuild ? null : <SettingsSection title={t("settings.remoteHosts")} sectionId="remoteHosts" titleHidden={titleHidden} grouped>
			<SettingsRow label={t("remote.enable")} description={t("remote.enableDescription")}>
				<Switch aria-label={t("remote.enable")} checked={enabled} onCheckedChange={setEnabled} />
			</SettingsRow>
			{signedIn ? <SettingsRow label={t("remote.cloudSync")} description={t("remote.cloudSyncDescription")}>
				<Switch aria-label={t("remote.cloudSync")} checked={cloudSync} onCheckedChange={(next) => { setCloudSync(next); requestRemoteHostsRefresh(); }} />
			</SettingsRow> : null}
			<p className="px-3 text-pretty text-xs leading-relaxed text-settings-muted">{t("remote.connectionDescription")}</p>
		</SettingsSection>}

		{isWebBuild ? null : <SettingsSection title={t("remote.tailscaleHosts")} grouped>
			{tailnet && !tailnet.available ? <SettingsRow label={t("remote.tailscaleUnavailable")} description={tailnet.error || t("remote.tailscaleUnavailableDescription")}>
				<span className="size-2 shrink-0 rounded-full bg-status-exited" aria-hidden="true" />
			</SettingsRow> : null}
			{loading && tailscaleHosts.length === 0 ? <p className="px-3 py-2 text-xs text-settings-muted" role="status">{t("remote.loadingHosts")}</p> : null}
			{!loading && tailscaleHosts.length === 0 && tailnet?.available ? <p className="px-3 py-2 text-pretty text-xs text-settings-muted">{t("remote.tailscaleEmpty")}</p> : null}
			{tailscaleHosts.map((host) => <SettingsRow icon={Server} key={host.hostId} label={host.label} description={t("remote.tailscaleHostDescription", { url: host.url })}>
				<div className="flex items-center gap-2">
					{/* One status slot: a dot, green when the last probe answered. */}
					<span className="flex items-center gap-1.5 text-xs text-settings-muted">
						<span className={cn("size-2 shrink-0 rounded-full", host.online ? "bg-status-ready" : "bg-muted-foreground/40")} aria-hidden="true" />
						{host.online ? t("remote.online") : t("remote.offline")}
					</span>
					<Button aria-label={t("remote.renameHost", { label: host.label })} disabled={busy || removeBusy} onClick={() => startEdit(host)} size="icon-sm" type="button" variant="ghost"><Pencil aria-hidden="true" className="size-icon-base" /></Button>
					<Button aria-label={t("remote.hideHost", { label: host.label })} disabled={busy || removeBusy} onClick={() => void setHidden(host, true)} size="sm" type="button" variant="ghost">{t("remote.hide")}</Button>
				</div>
			</SettingsRow>)}
			{hiddenHosts.length > 0 ? <>
				<p className="px-3 pt-2 text-xs text-settings-muted">{t("remote.hiddenHosts")}</p>
				{hiddenHosts.map((host) => <SettingsRow icon={Server} key={host.hostId} label={host.label} description={host.url}>
					<Button aria-label={t("remote.showHost", { label: host.label })} onClick={() => void setHidden(host, false)} size="sm" type="button" variant="outline">{t("remote.show")}</Button>
				</SettingsRow>)}
			</> : null}
			{hideError ? <p className="px-3 py-1 text-xs text-error" role="alert">{hideError}</p> : null}
		</SettingsSection>}

		<SettingsSection title={isWebBuild ? t("remote.savedHosts") : t("remote.pairedHosts")} grouped>
			{loading && pairedHosts.length === 0 ? <p className="px-3 py-2 text-xs text-settings-muted" role="status">{t("remote.loadingHosts")}</p> : null}
			{!loading && pairedHosts.length === 0 && !loadError ? <p className="px-3 py-2 text-pretty text-xs text-settings-muted">{t("remote.emptyHosts")}</p> : null}
			{pairedHosts.map((host) => <div key={host.url}>
				<SettingsRow icon={Server} label={host.label} description={host.url}>
					<div className="flex items-center gap-1">
						{/* The hub's registry has no edit: a host is removed and added again. */}
						{isWebBuild ? null : <Button aria-label={t("remote.editHost", { label: host.label })} disabled={busy || removeBusy} onClick={() => startEdit(host)} size="icon-sm" type="button" variant="ghost"><Pencil aria-hidden="true" className="size-icon-base" /></Button>}
						<Button aria-label={t("remote.removeHost", { label: host.label })} disabled={busy || removeBusy} onClick={() => {
							setConfirmingRemoval(host);
							setRemoveError("");
						}} size="icon-sm" type="button" variant="ghost"><Trash2 aria-hidden="true" className="size-icon-base" /></Button>
					</div>
				</SettingsRow>
				{!host.hostId ? <p className="px-3 py-1 text-xs text-settings-muted">{t("remote.repairRequired")}</p> : null}
				{/* Only plain http to another machine's address needs the switch. */}
				{isWebBuild || !needsPlainHTTPOptIn(host.url) ? null : <SettingsRow label={t("remote.allowPlainHTTP")} description={t("remote.allowPlainHTTPDescription")}>
					<div className="flex items-center gap-2">
						{host.allowInsecureHTTP ? null : <Badge variant="warning">{t("remote.reportsBlocked")}</Badge>}
						<Switch aria-label={t("remote.allowPlainHTTP")} checked={host.allowInsecureHTTP} disabled={busy || removeBusy} onCheckedChange={(checked) => void setPlainHTTPAllowed(host, checked)} />
					</div>
				</SettingsRow>}
			</div>)}
			{reportsError ? <p className="px-3 py-1 text-xs text-error" role="alert">{reportsError}</p> : null}
			{loadError ? <div className="flex items-center gap-2 px-3 py-2 text-xs text-error" role="alert">{loadError}<Button onClick={() => void load()} size="sm" type="button" variant="outline">{t("notify.retry")}</Button></div> : null}
		</SettingsSection>

		<SettingsSection title={editing ? (editing.tailscale ? t("remote.renameHost", { label: editing.label }) : t("remote.editHost", { label: editing.label })) : isWebBuild ? t("remote.addHost") : t("remote.addPairedHost")} grouped>
			{isWebBuild || editing?.tailscale ? null : <p className="px-3 text-pretty text-xs leading-relaxed text-settings-muted">{t("remote.setupHint")}</p>}
			<form className="flex flex-col gap-1.5" onSubmit={(event) => void save(event)} ref={formRef}>
				<SettingsRow label={t("remote.name")}><Input aria-label={t("remote.name")} className="max-w-sm" disabled={busy} maxLength={64} onChange={(event) => setLabel(event.target.value)} required value={label} /></SettingsRow>
				{editing?.tailscale ? null : <>
					<SettingsRow label={t("mobile.address")}><Input aria-label={t("mobile.address")} className="max-w-sm" disabled={busy} inputMode="url" onChange={(event) => setUrl(event.target.value)} placeholder={t("remote.addressPlaceholder")} required value={url} /></SettingsRow>
					<SettingsRow label={t("remote.connectionPassword")}><Input aria-label={t("remote.connectionPassword")} autoComplete="off" className="max-w-sm" disabled={busy} onChange={(event) => setPassword(event.target.value)} placeholder={editing?.hostId ? t("remote.keepPassword") : undefined} required={!editing?.hostId} type="password" value={password} /></SettingsRow>
				</>}
				{formError ? <p className="px-3 text-xs text-error" role="alert">{formError}</p> : null}
				<div className="flex justify-end gap-2 px-3 pt-1">
					{editing ? <Button disabled={busy} onClick={resetForm} size="sm" type="button" variant="outline">{t("confirm.cancel")}</Button> : null}
					<Button disabled={busy || !label.trim() || (!editing?.tailscale && !url.trim()) || (!editing?.hostId && !password)} size="sm" type="submit">{busy ? t("remote.saving") : editing ? t("remote.saveHost") : t("remote.addHost")}</Button>
				</div>
			</form>
		</SettingsSection>

		<ConfirmDialog
			busy={removeBusy}
			confirmLabel={t("shell.remove")}
			description={t("remote.removeDescription")}
			destructive
			error={removeError || null}
			onConfirm={() => void remove()}
			onOpenChange={(open) => { if (!open && !removeBusy) setConfirmingRemoval(null); }}
			open={confirmingRemoval !== null}
			title={t("remote.removeTitle", { label: confirmingRemoval?.label })}
		/>
	</>;
}

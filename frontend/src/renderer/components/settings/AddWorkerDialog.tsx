import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { hubHostsQueryKey, useHubHosts, type HubHost } from "../../hooks/useHubHosts";
import type { MessageKey } from "../../i18n";
import { Button } from "../ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
	settingsDialogBodyClass,
	settingsDialogContentClass,
	settingsDialogFooterClass,
	settingsDialogHeaderClass,
} from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../ui/tabs";

export type WorkerDialogValues = {
	label: string;
	claudeApiKey: string;
	claudeOAuthToken: string;
	githubToken: string;
	gitName: string;
	gitEmail: string;
	url: string;
	password: string;
};

export type WorkerDialogMode = "railway" | "existing";

export const GITHUB_WORKER_TOKEN_PATTERN = /^(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,})$/;
const WORKER_LABEL_PATTERN = /^[a-z0-9-]{3,32}$/;

export function validateWorkerDialog(mode: WorkerDialogMode, values: WorkerDialogValues, hosts: readonly HubHost[]): Partial<Record<keyof WorkerDialogValues, string>> {
	const errors: Partial<Record<keyof WorkerDialogValues, string>> = {};
	const label = values.label.trim();
	if (!WORKER_LABEL_PATTERN.test(label)) errors.label = "settings.workers.validation.labelFormat";
	else if (label.toLowerCase() === "hub" || hosts.some((host) => host.label.toLowerCase() === label.toLowerCase())) errors.label = "settings.workers.validation.labelTaken";
	if (mode === "existing") {
		if (!values.url.trim()) errors.url = "settings.workers.validation.urlRequired";
		else {
			try {
				const url = new URL(values.url.trim());
				if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || url.pathname !== "/" || url.search || url.hash) errors.url = "settings.workers.validation.urlOrigin";
			} catch { errors.url = "settings.workers.validation.urlInvalid"; }
		}
		if (!values.password) errors.password = "settings.workers.validation.passwordRequired";
		return errors;
	}
	if (!values.claudeApiKey.trim() && !values.claudeOAuthToken.trim()) errors.claudeApiKey = "settings.workers.validation.claudeRequired";
	if (!values.githubToken.trim()) errors.githubToken = "settings.workers.validation.githubRequired";
	else if (!GITHUB_WORKER_TOKEN_PATTERN.test(values.githubToken.trim())) errors.githubToken = "settings.workers.validation.githubInvalid";
	if (!values.gitName.trim()) errors.gitName = "settings.workers.validation.gitNameRequired";
	if (!values.gitEmail.trim() || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(values.gitEmail.trim())) errors.gitEmail = "settings.workers.validation.gitEmailInvalid";
	return errors;
}

export function workerRequestPayload(mode: WorkerDialogMode, values: WorkerDialogValues): { path: string; body: Record<string, unknown> } {
	if (mode === "existing") return { path: "/hub/hosts", body: { label: values.label.trim(), url: values.url.trim(), password: values.password } };
	return {
		path: "/hub/workers",
		body: {
			label: values.label.trim(),
			env: {
				...(values.claudeApiKey.trim() ? { ANTHROPIC_API_KEY: values.claudeApiKey.trim() } : {}),
				...(values.claudeOAuthToken.trim() ? { CLAUDE_CODE_OAUTH_TOKEN: values.claudeOAuthToken.trim() } : {}),
				GH_TOKEN: values.githubToken.trim(),
				AO_GIT_USER_NAME: values.gitName.trim(),
				AO_GIT_USER_EMAIL: values.gitEmail.trim(),
			},
		},
	};
}

export async function submitWorker(mode: WorkerDialogMode, values: WorkerDialogValues, fetchImpl: typeof fetch = fetch): Promise<void> {
	const { path, body } = workerRequestPayload(mode, values);
	const response = await fetchImpl(`${(import.meta.env.VITE_AO_API_BASE_URL ?? "").replace(/\/+$/, "")}${path}`, {
		method: "POST",
		credentials: "same-origin",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(body),
	});
	if (response.ok) return;
	const result = await response.json().catch(() => null) as { error?: string; message?: string; count?: number; limit?: number } | null;
	if (response.status === 429) throw new Error(result?.message ?? `Worker limit reached (${result?.count ?? "?"}/${result?.limit ?? "?"})`);
	if (response.status >= 500) throw new Error(result?.message ?? result?.error ?? "worker_request_failed");
	throw new Error(result?.message ?? result?.error ?? `Could not add worker (HTTP ${response.status})`);
}

const emptyValues = (): WorkerDialogValues => ({ label: "", claudeApiKey: "", claudeOAuthToken: "", githubToken: "", gitName: "", gitEmail: "", url: "", password: "" });

export function AddWorkerDialog() {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const { data } = useHubHosts();
	const [open, setOpen] = useState(false);
	const [mode, setMode] = useState<WorkerDialogMode>("railway");
	const [values, setValues] = useState<WorkerDialogValues>(emptyValues);
	const [errors, setErrors] = useState<Partial<Record<keyof WorkerDialogValues, string>>>({});
	const [requestError, setRequestError] = useState("");
	const [submitting, setSubmitting] = useState(false);
	const hosts = data?.workers ?? [];

	function close(next: boolean) {
		setOpen(next);
		if (!next) {
			setValues(emptyValues());
			setErrors({});
			setRequestError("");
			setMode("railway");
		}
	}
	function update(field: keyof WorkerDialogValues, value: string) {
		setValues((current) => ({ ...current, [field]: value }));
		setErrors((current) => ({ ...current, [field]: undefined }));
		setRequestError("");
	}
	async function submit(event: React.FormEvent<HTMLFormElement>) {
		event.preventDefault();
		const nextErrors = validateWorkerDialog(mode, values, hosts);
		setErrors(nextErrors);
		if (Object.keys(nextErrors).length) return;
		setSubmitting(true);
		setRequestError("");
		try {
			await submitWorker(mode, values);
			close(false);
			void queryClient.invalidateQueries({ queryKey: hubHostsQueryKey });
		} catch (error) {
			const message = error instanceof Error ? error.message : "worker_request_failed";
			setRequestError(message === "worker_provisioning_unavailable" ? t("settings.workers.provisionUnavailable") : message === "worker_request_failed" ? t("settings.workers.requestFailed") : message);
		} finally {
			setSubmitting(false);
		}
	}

	const field = (name: keyof WorkerDialogValues, label: string, type = "text", secret = false, placeholder?: string) => (
		<div className="flex flex-col gap-1.5" key={name}>
			<Label htmlFor={`worker-${name}`}>{label}</Label>
			<Input id={`worker-${name}`} type={secret ? "password" : type} autoComplete="off" spellCheck={false} placeholder={placeholder} value={values[name]} onChange={(event) => update(name, event.target.value)} aria-invalid={Boolean(errors[name])} />
			{errors[name] ? <p className="text-xs text-error" role="alert">{t(errors[name] as MessageKey)}</p> : null}
		</div>
	);

	return <>
		<Button onClick={() => setOpen(true)} size="sm" type="button">{t("settings.workers.add")}</Button>
		<Dialog open={open} onOpenChange={close}>
			<DialogContent className={`${settingsDialogContentClass} w-[min(34rem,calc(100vw-var(--space-8)))]`}>
				<DialogHeader className={settingsDialogHeaderClass}>
					<DialogTitle>{t("settings.workers.addTitle")}</DialogTitle>
					<DialogDescription className="sr-only">{t("settings.workers.addDescription")}</DialogDescription>
				</DialogHeader>
				<form className="flex min-h-0 flex-1 flex-col" onSubmit={(event) => void submit(event)}>
					<div className={settingsDialogBodyClass}>
						<Tabs value={mode} onValueChange={(value) => { setMode(value as WorkerDialogMode); setErrors({}); setRequestError(""); }}>
							<TabsList aria-label={t("settings.workers.addMethods")} className="w-full">
								<TabsTrigger value="railway">{t("settings.workers.createRailway")}</TabsTrigger>
								<TabsTrigger value="existing">{t("settings.workers.addExisting")}</TabsTrigger>
							</TabsList>
							<TabsContent value="railway" className="flex flex-col gap-3 pt-3">
								{field("label", t("settings.workers.label"), "text", false, "worker-1")}
								{field("claudeApiKey", t("settings.workers.claudeApiKey"), "text", true)}
								{field("claudeOAuthToken", t("settings.workers.claudeOAuthToken"), "text", true)}
								{field("githubToken", "GH_TOKEN", "text", true, "github_pat_…")}
								{field("gitName", t("settings.workers.gitName"))}
								{field("gitEmail", t("settings.workers.gitEmail"), "email")}
							</TabsContent>
							<TabsContent value="existing" className="flex flex-col gap-3 pt-3">
								{field("label", t("settings.workers.label"), "text", false, "worker-1")}
								{field("url", t("settings.workers.address"), "url", false, "https://worker.example.com")}
								{field("password", t("settings.workers.password"), "text", true)}
							</TabsContent>
						</Tabs>
						{requestError ? <p className="text-sm text-error" role="alert">{requestError}</p> : null}
					</div>
					<DialogFooter className={settingsDialogFooterClass}>
						<Button disabled={submitting} onClick={() => close(false)} type="button" variant="footer">{t("confirm.cancel")}</Button>
						<Button disabled={submitting} type="submit" variant="footer-primary">{submitting ? t("settings.workers.adding") : t("settings.workers.add")}</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	</>;
}

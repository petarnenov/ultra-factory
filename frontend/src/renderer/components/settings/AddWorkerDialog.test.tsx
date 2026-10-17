import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AddWorkerDialog, validateWorkerDialog, workerRequestPayload, type WorkerDialogValues } from "./AddWorkerDialog";
import type { HubHost } from "../../hooks/useHubHosts";

const values: WorkerDialogValues = {
	label: "worker-1",
	claudeApiKey: "sk-ant-example",
	claudeOAuthToken: "",
	githubToken: `github_pat_${"A".repeat(80)}`,
	gitName: "AO Worker",
	gitEmail: "worker@example.com",
	url: "https://worker.example.com",
	password: "worker-secret",
};

const hosts: HubHost[] = [{
	id: "host-1", label: "worker-existing", url: "https://existing.example.com", source: "manual", status: "online",
	createdAt: "2026-10-16T08:00:00.000Z", removable: true,
}];

describe("AddWorkerDialog validation", () => {
	it("validates the worker label, uniqueness, Claude credential, and GitHub token", () => {
		expect(validateWorkerDialog("railway", values, hosts)).toEqual({});
		expect(validateWorkerDialog("railway", { ...values, label: "worker-existing" }, hosts).label).toBe("settings.workers.validation.labelTaken");
		expect(validateWorkerDialog("railway", { ...values, label: "Bad Label", claudeApiKey: "", claudeOAuthToken: "", githubToken: "bad" }, hosts)).toMatchObject({
			label: "settings.workers.validation.labelFormat",
			claudeApiKey: "settings.workers.validation.claudeRequired",
			githubToken: "settings.workers.validation.githubInvalid",
		});
	});

	it("validates an existing host address and password", () => {
		expect(validateWorkerDialog("existing", values, hosts)).toEqual({});
		expect(validateWorkerDialog("existing", { ...values, url: "javascript:alert(1)", password: "" }, hosts)).toMatchObject({
			url: "settings.workers.validation.urlOrigin",
			password: "settings.workers.validation.passwordRequired",
		});
	});
});

describe("workerRequestPayload", () => {
	it("sends the Railway worker contract without size or secrets outside env", () => {
		expect(workerRequestPayload("railway", values)).toEqual({
			path: "/hub/workers",
			body: {
				label: "worker-1",
				env: {
					ANTHROPIC_API_KEY: "sk-ant-example",
					GH_TOKEN: values.githubToken,
					AO_GIT_USER_NAME: "AO Worker",
					AO_GIT_USER_EMAIL: "worker@example.com",
				},
			},
		});
	});

	it("sends an existing worker to the hosts registry contract", () => {
		expect(workerRequestPayload("existing", values)).toEqual({
			path: "/hub/hosts",
			body: { label: "worker-1", url: "https://worker.example.com", password: "worker-secret" },
		});
	});
});

describe("worker submission", () => {
	it("posts the expected payload and does not retain a response body", async () => {
		const fetchMock = vi.fn(async () => new Response(JSON.stringify({ worker: { id: "worker-1", password: "never-returned" } }), { status: 202 }));
		const { submitWorker } = await import("./AddWorkerDialog");
		await submitWorker("railway", values, fetchMock as unknown as typeof fetch);
		const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
		expect(url).toBe("/hub/workers");
		expect(init.method).toBe("POST");
		expect(JSON.parse(String(init.body))).toEqual(workerRequestPayload("railway", values).body);
		expect(String(init.body)).not.toContain("worker-secret");
	});
});

describe("AddWorkerDialog secrets", () => {
	it("uses password inputs without autocomplete and clears them after close", async () => {
		vi.stubGlobal("fetch", vi.fn(async () => Response.json({ workers: [], orphans: [], workersByStatus: {}, limit: 5 })));
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		render(<QueryClientProvider client={queryClient}><AddWorkerDialog /></QueryClientProvider>);
		fireEvent.click(screen.getByRole("button", { name: "Add worker" }));
		const keyField = await screen.findByLabelText("Claude API key");
		const tokenField = screen.getByLabelText("GH_TOKEN");
		expect(keyField).toHaveAttribute("type", "password");
		expect(keyField).toHaveAttribute("autoComplete", "off");
		expect(tokenField).toHaveAttribute("type", "password");
		expect(tokenField).toHaveAttribute("autoComplete", "off");
		fireEvent.change(keyField, { target: { value: "secret-key" } });
		fireEvent.click(screen.getByRole("button", { name: "Close" }));
		fireEvent.click(screen.getByRole("button", { name: "Add worker" }));
		expect(await screen.findByLabelText("Claude API key")).toHaveValue("");
		expect(screen.getByLabelText("GH_TOKEN")).toHaveValue("");
		vi.unstubAllGlobals();
	});
});

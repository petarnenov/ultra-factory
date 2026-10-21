import { act, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { useLocalSignInDialogStore } from "../stores/local-signin-dialog-store";
import { useUiStore } from "../stores/ui-store";

const gate = vi.hoisted(() => ({ cloudEnabled: false }));
vi.mock("../lib/bridge", () => ({ aoBridge: { remotes: { list: vi.fn(async () => []) } } }));
vi.mock("../lib/cloud-session", () => ({ useCloudSession: () => ({ status: "unauthenticated", signIn: vi.fn() }) }));
vi.mock("../hooks/useCloudGate", () => ({ useCloudGate: () => ({ cloudEnabled: gate.cloudEnabled, localEnabled: true, client: "" }) }));
vi.mock("../hooks/useCloudLocalAuth", () => ({
	useCloudLocalAuth: () => ({ available: true, cpUrl: "http://127.0.0.1:8787", login: vi.fn(), register: vi.fn() }),
}));
vi.mock("../hooks/useSettings", () => ({
	useSettings: () => ({ settings: { cloudEnabled: gate.cloudEnabled, cloudControlPlaneUrl: "http://127.0.0.1:8787" } }),
}));

import { CloudOnboardingGate } from "./CloudOnboardingGate";
import { RemoteHostsSettings } from "./settings/RemoteHostsSettings";

const renderApp = () => render(
	<QueryClientProvider client={new QueryClient()}>
		<CloudOnboardingGate />
		<RemoteHostsSettings />
	</QueryClientProvider>,
);

afterEach(() => {
	gate.cloudEnabled = false;
	useUiStore.setState({ developerMode: false });
	act(() => useLocalSignInDialogStore.getState().closeDialog());
});

it("opens the local sign-in dialog from Remote hosts in developer mode with the cloud offering off", () => {
	useUiStore.setState({ developerMode: true });
	renderApp();
	fireEvent.click(screen.getByRole("button", { name: "Sign in to AO Cloud" }));
	expect(screen.getByRole("dialog")).toBeVisible();
});

it("does not mount the local sign-in dialog when neither the cloud offering nor developer mode is on", () => {
	render(<QueryClientProvider client={new QueryClient()}><CloudOnboardingGate /></QueryClientProvider>);
	act(() => useLocalSignInDialogStore.getState().openDialog());
	expect(screen.queryByRole("dialog")).toBeNull();
});

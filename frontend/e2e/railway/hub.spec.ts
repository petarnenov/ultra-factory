import { expect, test } from "@playwright/test";

const operatorPassword = "e2e-operator-password";
const workerPassword = "e2e-worker-password";
const dummyGitToken = "github_pat_0000000000000000000000000000000000000000";

test("logs in, connects and provisions a worker, uses its terminal, then removes it", async ({ page }) => {
	await page.goto("/login");
	await page.getByLabel(/password/i).fill(operatorPassword);
	await page.getByRole("button", { name: /sign in|log in/i }).click();
	await expect(page).not.toHaveURL(/\/login/);

	const manual = await page.request.post("/hub/hosts", {
		headers: { Origin: "http://localhost:8080" },
		data: { label: "worker-manual", url: "http://worker-1:3011", password: workerPassword },
	});
	expect(manual.status()).toBe(201);
	const provision = await page.request.post("/hub/workers", {
		headers: { Origin: "http://localhost:8080" },
		data: {
			label: "worker-e2e",
			env: { GH_TOKEN: dummyGitToken, AO_GIT_USER_NAME: "AO E2E", AO_GIT_USER_EMAIL: "e2e@example.invalid" },
		},
	});
	expect(provision.status()).toBe(202);
	const { worker } = await provision.json();
	await expect.poll(async () => {
		const result = await page.request.get("/hub/workers");
		const data = await result.json();
		return data.workers.find((item: { id: string }) => item.id === worker.id)?.status;
	}, { timeout: 30_000 }).toBe("online");
	const workerList = await (await page.request.get("/hub/workers")).json();
	const registered = workerList.workers.find((item: { id: string }) => item.id === worker.id);
	expect(registered.hostId).toBeTruthy();

	const hostPage = await page.request.get(`/host/${registered.hostId}`);
	expect(hostPage.status()).toBeLessThan(500);
	const clone = await page.request.post(`/hosts/${registered.hostId}/api/v1/projects/clone`, {
		headers: { Origin: "http://localhost:8080" },
		data: { name: "railway-e2e-fixture", remoteUrl: "file:///fixtures/e2e-fixture.git", destinationParent: "/data/projects" },
	});
	expect(clone.status()).toBe(201);
	const { project } = await clone.json();
	const sessionResponse = await page.request.post(`/hosts/${registered.hostId}/api/v1/sessions`, {
		headers: { Origin: "http://localhost:8080" },
		data: { clientRequestId: "railway-e2e-session", projectId: project.id, kind: "worker", harness: "claude-code", mode: "tui", prompt: "Print the fake-agent ready output.", displayName: "Railway e2e session" },
	});
	expect(sessionResponse.status()).toBe(201);
	const { session } = await sessionResponse.json();

	await page.goto(`/host/${registered.hostId}`);
	await expect(page).toHaveURL(new RegExp(`/host/${registered.hostId}$`));
	const terminalOutput = await page.evaluate(({ hostId, terminalHandleId }) => new Promise<string>((resolve, reject) => {
		const socket = new WebSocket(`ws://localhost:8080/hosts/${encodeURIComponent(hostId)}/mux`);
		let output = "";
		const timeout = window.setTimeout(() => { socket.close(); reject(new Error(`Terminal output timed out: ${output}`)); }, 30_000);
		socket.addEventListener("open", () => socket.send(JSON.stringify({ ch: "terminal", type: "open", id: terminalHandleId, cols: 100, rows: 30 })));
		socket.addEventListener("message", (event) => {
			const frame = JSON.parse(String(event.data)) as { ch?: string; type?: string; data?: string; error?: string };
			if (frame.error) { window.clearTimeout(timeout); socket.close(); reject(new Error(frame.error)); return; }
			if (frame.ch === "terminal" && frame.type === "data" && frame.data) {
				output += atob(frame.data);
				if (output.includes("AO smoke agent ready")) { window.clearTimeout(timeout); socket.close(); resolve(output); }
			}
		});
		socket.addEventListener("error", () => { window.clearTimeout(timeout); reject(new Error("Terminal mux connection failed")); });
	}), { hostId: registered.hostId, terminalHandleId: session.terminalHandleId });
	expect(terminalOutput).toContain("AO smoke agent ready");

	await page.goto("/");
	await page.getByRole("button", { name: /settings/i }).click();
	await page.getByRole("button", { name: /workers/i }).click();
	await expect(page.getByText("worker-e2e")).toBeVisible();
	await page.getByRole("button", { name: /remove/i }).last().click();
	const dialog = page.getByRole("dialog");
	await expect(dialog).toBeVisible();
	const force = dialog.getByRole("checkbox");
	if (await force.count()) await force.check();
	await expect(dialog.getByRole("button", { name: /remove|delete/i })).toBeVisible();
	const confirm = dialog.getByRole("button", { name: /remove|delete/i });
	if (await dialog.getByLabel(/type .*worker-e2e/i).count()) await dialog.getByLabel(/type .*worker-e2e/i).fill("worker-e2e");
	await confirm.click();
	await expect(page.getByText("worker-e2e")).toHaveCount(0, { timeout: 15_000 });
});

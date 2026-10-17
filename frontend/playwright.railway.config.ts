import { defineConfig } from "@playwright/test";

export default defineConfig({
	testDir: "e2e/railway",
	timeout: 120_000,
	workers: 1,
	use: { baseURL: "http://localhost:8080" },
});

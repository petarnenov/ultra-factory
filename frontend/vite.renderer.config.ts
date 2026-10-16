// defineConfig comes from vitest/config (a superset of vite's) so the `test`
// block typechecks; vitest itself must be pointed at this file explicitly
// (package.json test script) because it only auto-discovers vite.config.*.
import { execFileSync } from "node:child_process";
import { defineConfig } from "vitest/config";
import type { Plugin } from "vite";
import { fileURLToPath, URL } from "node:url";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { contentSecurityPolicy } from "./vite.renderer.csp";

function gitValue(args: string[], fallback: string): string {
	try {
		const value = execFileSync("git", args, {
			cwd: fileURLToPath(new URL("../", import.meta.url)),
			encoding: "utf8",
			stdio: ["ignore", "pipe", "ignore"],
		}).trim();
		return value || fallback;
	} catch {
		return fallback;
	}
}

function gitIsDirty(): boolean {
	try {
		return execFileSync("git", ["status", "--porcelain", "--untracked-files=all"], {
			cwd: fileURLToPath(new URL("../", import.meta.url)),
			encoding: "utf8",
			stdio: ["ignore", "pipe", "ignore"],
		}).trim().length > 0;
	} catch {
		return false;
	}
}

const DEV_BUILD_INFO = {
	branch: gitValue(["branch", "--show-current"], "main"),
	commit: gitValue(["rev-parse", "--short", "HEAD"], "unknown"),
	worktree: gitValue(["rev-parse", "--show-toplevel"], "unknown"),
	isDirty: gitIsDirty(),
};

const injectCspMeta: Plugin = {
	name: "inject-csp-meta",
	apply: "build",
	transformIndexHtml() {
		return [
			{
				tag: "meta",
				attrs: { "http-equiv": "Content-Security-Policy", content: contentSecurityPolicy("build") },
				injectTo: "head-prepend",
			},
		];
	},
};

const productUiReactBoundary: Plugin = {
	name: "product-ui-react-boundary",
	enforce: "pre",
	async resolveId(source, importer) {
		if (!importer?.includes("/packages/product-ui/")) {
			return null;
		}
		const remap =
			source === "react" ||
			source.startsWith("react/") ||
			source === "react-dom" ||
			source.startsWith("react-dom/") ||
			source === "motion" ||
			source.startsWith("motion/");
		if (!remap) {
			return null;
		}
		return this.resolve(
			source,
			fileURLToPath(new URL("./src/renderer/main.tsx", import.meta.url)),
			{ skipSelf: true },
		);
	},
};

// VITE_AO_WEB=1 selects the standalone web target (build:web, dev:web:real):
// the real renderer served from a static host behind a proxy to the daemon, as
// opposed to the Electron renderer or the VITE_NO_ELECTRON mock preview.
const isWebTarget = process.env.VITE_AO_WEB === "1";
const devApiTarget = process.env.AO_DEV_API_TARGET ?? "http://127.0.0.1:3001";

export default defineConfig({
	// Relative asset URLs so dist-web works from any mount path; the router uses
	// hash history, so no server-side route fallback is needed.
	base: isWebTarget ? "./" : undefined,
	build: {
		outDir: isWebTarget ? "dist-web" : undefined,
	},
	define: {
		"import.meta.env.VITE_AO_GIT_BRANCH": JSON.stringify(DEV_BUILD_INFO.branch),
		"import.meta.env.VITE_AO_GIT_COMMIT": JSON.stringify(DEV_BUILD_INFO.commit),
		"import.meta.env.VITE_AO_GIT_WORKTREE": JSON.stringify(DEV_BUILD_INFO.worktree),
		"import.meta.env.VITE_AO_GIT_DIRTY": JSON.stringify(String(DEV_BUILD_INFO.isDirty)),
	},
	// "@/" → the renderer root (src/renderer), the shadcn/ui import convention.
	resolve: {
		alias: {
			"@": fileURLToPath(new URL("./src/renderer", import.meta.url)),
			"@aoagents/product-ui": fileURLToPath(
				new URL("../packages/product-ui/src/index.ts", import.meta.url),
			),
			// The alias above resolves product-ui to its source, so that package's
			// own imports resolve from packages/product-ui/ — which only has a
			// node_modules if `npm ci` was run there too. CI does that; a
			// frontend-only install does not, and the failure mode is quiet: every
			// test importing product-ui dies at transform time with "failed to
			// resolve clsx", which reads as pre-existing breakage rather than a
			// missing install. Point both runtime deps at the frontend copies so
			// one install is enough.
			clsx: fileURLToPath(new URL("./node_modules/clsx", import.meta.url)),
			"tailwind-merge": fileURLToPath(
				new URL("./node_modules/tailwind-merge", import.meta.url),
			),
		},
	},
	// Dev proxy for browser runs (VITE_NO_ELECTRON=1 preview, VITE_AO_WEB=1
	// dev:web:real) — forwards /api, /mux and the daemon health probes to the
	// daemon so the renderer can be tested against a running daemon from a
	// plain browser without an Electron shell.
	server: {
		proxy: {
			"/api": { target: devApiTarget, changeOrigin: false },
			"/mux": { target: devApiTarget, changeOrigin: false, ws: true },
			"/readyz": { target: devApiTarget, changeOrigin: false },
			"/healthz": { target: devApiTarget, changeOrigin: false },
		},
	},
	plugins: [
		TanStackRouterVite({
			routesDirectory: "./src/renderer/routes",
			generatedRouteTree: "./src/renderer/routeTree.gen.ts",
			target: "react",
			autoCodeSplitting: true,
		}),
		productUiReactBoundary,
		react(),
		tailwindcss(),
		injectCspMeta,
	],
	test: {
		environment: "jsdom",
		testTimeout: 20_000,
		// Anchor node_modules at any depth: a bare "node_modules/**" replaces
		// vitest's default "**/node_modules/**" and only matches the root.
		exclude: ["**/node_modules/**", "dist/**", "dist-web/**", "dist-electron/**", "e2e/**"],
		globals: true,
		setupFiles: "./src/renderer/test/setup.ts",
	},
});

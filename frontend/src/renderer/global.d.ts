import type { AoBridge } from "../preload";

declare global {
	interface Window {
		ao?: AoBridge;
	}

	interface ImportMetaEnv {
		readonly VITE_AO_POSTHOG_KEY?: string;
		readonly VITE_AO_POSTHOG_HOST?: string;
		readonly VITE_AO_GIT_BRANCH?: string;
		readonly VITE_AO_GIT_COMMIT?: string;
		readonly VITE_AO_GIT_WORKTREE?: string;
		readonly VITE_AO_GIT_DIRTY?: string;
		readonly VITE_AO_DEVELOPER_MODE_DEFAULT?: string;
		readonly VITE_AO_API_BASE_URL?: string;
		readonly VITE_AO_WEB?: string;
	}
}

export {};

import type { ModelContextLike, ToolSummary } from "./model-context";

declare global {
	interface Document {
		modelContext?: ModelContextLike;
	}

	interface Window {
		__aoWebMcp?: {
			listTools(): ToolSummary[];
			callTool(name: string, args?: unknown): Promise<unknown>;
		};
	}
}

export {};

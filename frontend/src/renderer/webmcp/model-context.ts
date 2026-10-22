import { usesPreviewWorkspaceData } from "../lib/preview-mode";

export type JsonSchema = {
	type: "object";
	properties?: Record<string, { type: string; description?: string; enum?: string[] }>;
	required?: string[];
};

export type ToolDescriptor<A = unknown, R = unknown> = {
	// "<page>.<verb>", e.g. "board.read".
	name: string;
	// One sentence, imperative.
	description: string;
	inputSchema: JsonSchema;
	execute: (args: A) => Promise<R> | R;
};

export type ToolSummary = Pick<ToolDescriptor, "name" | "description" | "inputSchema">;

export type ToolErrorCode = "unknown_tool" | "invalid_args" | "precondition";

// Tools throw this when the UI state does not allow the requested action.
export class ToolError extends Error {
	readonly code: ToolErrorCode;

	constructor(code: ToolErrorCode, message: string) {
		super(message);
		this.name = "ToolError";
		this.code = code;
	}
}

export type ModelContextLike = {
	registerTool(descriptor: ToolDescriptor): void | Promise<void>;
	unregisterTool(name: string): void | Promise<void>;
};

type Registration = { descriptor: ToolDescriptor };

// Each registration is a distinct object, so a stale cleanup can tell that
// the name now belongs to a newer registration and leave it alone.
const registry = new Map<string, Registration>();

// Captured at load: installModelContext() replaces document.modelContext with
// the shim, which must not be mistaken for a native implementation later.
const native = detectNative();

function detectNative(): ModelContextLike | undefined {
	const candidate =
		document.modelContext ?? (navigator as { modelContext?: ModelContextLike }).modelContext;
	return typeof candidate?.registerTool === "function" ? candidate : undefined;
}

function forwardNative(
	op: keyof ModelContextLike,
	run: (target: ModelContextLike) => void | Promise<void>,
): void {
	if (!native || typeof native[op] !== "function") return;
	const warn = (error: unknown) => console.warn(`[webmcp] native ${op} failed`, error);
	try {
		// Native results may arrive later; they must not become unhandled rejections.
		void Promise.resolve(run(native)).catch(warn);
	} catch (error) {
		warn(error);
	}
}

export function registerTool<A, R>(descriptor: ToolDescriptor<A, R>): () => void {
	const tool = descriptor as ToolDescriptor;
	if (registry.has(tool.name)) {
		if (import.meta.env.DEV) console.warn(`[webmcp] replacing tool "${tool.name}"`);
		forwardNative("unregisterTool", (target) => target.unregisterTool(tool.name));
	}
	const registration: Registration = { descriptor: tool };
	registry.set(tool.name, registration);
	forwardNative("registerTool", (target) => target.registerTool(tool));
	return () => {
		if (registry.get(tool.name) === registration) unregisterTool(tool.name);
	};
}

export function unregisterTool(name: string): void {
	if (!registry.delete(name)) return;
	forwardNative("unregisterTool", (target) => target.unregisterTool(name));
}

export function listTools(): ToolSummary[] {
	return [...registry.values()]
		.map(({ descriptor: { name, description, inputSchema } }) => ({ name, description, inputSchema }))
		.sort((a, b) => a.name.localeCompare(b.name));
}

export async function callTool(name: string, args: unknown = {}): Promise<unknown> {
	const registration = registry.get(name);
	if (!registration) throw new ToolError("unknown_tool", name);
	const problem = validateArgs(registration.descriptor.inputSchema, args);
	if (problem) throw new ToolError("invalid_args", `${name}: ${problem}`);
	return registration.descriptor.execute(args);
}

export function installModelContext(): void {
	if (!native) {
		document.modelContext = {
			registerTool: (descriptor) => void registerTool(descriptor),
			unregisterTool,
		};
	}
	if (import.meta.env.DEV || usesPreviewWorkspaceData) {
		window.__aoWebMcp = { listTools, callTool };
	}
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

// Minimal checker: object shape, required keys, property types and enums.
// It deliberately ignores extra keys and unsupported keywords.
function validateArgs(schema: JsonSchema, args: unknown): string | null {
	if (!isRecord(args)) return "arguments must be an object";
	for (const key of schema.required ?? []) {
		if (args[key] === undefined) return `missing required "${key}"`;
	}
	for (const [key, property] of Object.entries(schema.properties ?? {})) {
		const value = args[key];
		if (value === undefined) continue;
		if (!matchesType(property.type, value)) return `"${key}" must be ${property.type}`;
		if (property.enum && !property.enum.some((option) => option === value)) {
			return `"${key}" must be one of ${property.enum.join(", ")}`;
		}
	}
	return null;
}

function matchesType(type: string, value: unknown): boolean {
	switch (type) {
		case "string":
			return typeof value === "string";
		case "number":
			return typeof value === "number" && Number.isFinite(value);
		case "boolean":
			return typeof value === "boolean";
		case "object":
			return isRecord(value);
		case "array":
			return Array.isArray(value);
		default:
			// Unknown schema types fail closed rather than pass unchecked.
			return false;
	}
}

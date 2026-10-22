import { useEffect } from "react";
import { registerTool, type ToolDescriptor } from "./model-context";

// Registers while the component is mounted; deps decide when it re-registers.
export function useModelContextTool<A, R>(descriptor: ToolDescriptor<A, R> | null, deps: unknown[]): void {
	useEffect(() => {
		if (!descriptor) return undefined;
		return registerTool(descriptor);
	}, deps);
}

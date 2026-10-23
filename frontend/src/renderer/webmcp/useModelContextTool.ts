import { useEffect } from "react";
import { registerTool, type ToolDescriptor } from "./model-context";

// Registers a set of tools as one unit; deps decide when the set re-registers.
export function useModelContextTools(descriptors: ToolDescriptor[], deps: unknown[]): void {
	useEffect(() => {
		const releases = descriptors.map((descriptor) => registerTool(descriptor));
		return () => releases.forEach((release) => release());
	}, deps);
}

// Registers while the component is mounted; deps decide when it re-registers.
export function useModelContextTool<A, R>(descriptor: ToolDescriptor<A, R> | null, deps: unknown[]): void {
	useEffect(() => {
		if (!descriptor) return undefined;
		return registerTool(descriptor);
	}, deps);
}

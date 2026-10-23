import { createContext, useContext, type CSSProperties, type ReactElement, type ReactNode } from "react";
import * as RechartsPrimitive from "recharts";
import { cn } from "../../lib/utils";

export type ChartConfig = Record<string, { label: ReactNode; color?: string }>;

const ChartConfigContext = createContext<ChartConfig>({});

// Series colors become wrapper variables for Recharts marks. This avoids
// injecting a <style> element and stays within the existing CSP.
function colorVariables(config: ChartConfig): CSSProperties {
	const variables: Record<string, string> = {};
	for (const [key, { color }] of Object.entries(config)) {
		if (color) variables[`--color-${key}`] = color;
	}
	return variables as CSSProperties;
}

function ChartContainer({
	config,
	className,
	children,
}: {
	config: ChartConfig;
	className?: string;
	children: ReactElement;
}) {
	return (
		<ChartConfigContext.Provider value={config}>
			<div
				data-slot="chart"
				className={cn(
					"flex aspect-video justify-center text-caption [&_.recharts-text]:fill-muted-foreground",
					className,
				)}
				style={colorVariables(config)}
			>
				<RechartsPrimitive.ResponsiveContainer>{children}</RechartsPrimitive.ResponsiveContainer>
			</div>
		</ChartConfigContext.Provider>
	);
}

const ChartTooltip = RechartsPrimitive.Tooltip;

function ChartTooltipContent({
	active,
	payload,
	label,
}: RechartsPrimitive.TooltipContentProps<number, string>) {
	const config = useContext(ChartConfigContext);
	if (!active || !payload?.length) return null;

	return (
		<div className="grid min-w-32 gap-1 rounded-md border border-border bg-popover px-2.5 py-1.5 text-caption text-popover-foreground">
			{label != null && <div className="font-medium">{label}</div>}
			{payload.map((item) => (
				<div key={item.name} className="flex items-center justify-between gap-4">
					<span className="flex items-center gap-1.5">
						<span className="size-2 rounded-sm" style={{ backgroundColor: item.color }} />
						{config[String(item.dataKey)]?.label ?? item.name}
					</span>
					<span className="font-mono tabular-nums">{item.value}</span>
				</div>
			))}
		</div>
	);
}

const ChartLegend = RechartsPrimitive.Legend;

function ChartLegendContent({ payload }: RechartsPrimitive.DefaultLegendContentProps) {
	const config = useContext(ChartConfigContext);
	if (!payload?.length) return null;

	return (
		<div className="flex flex-wrap items-center justify-center gap-4 text-caption text-muted-foreground">
			{payload.map((item) => (
				<div key={String(item.value)} className="flex items-center gap-1.5">
					<span className="size-2 rounded-sm" style={{ backgroundColor: item.color }} />
					{config[String(item.dataKey)]?.label ?? item.value}
				</div>
			))}
		</div>
	);
}

export {
	ChartContainer,
	ChartLegend,
	ChartLegendContent,
	ChartTooltip,
	ChartTooltipContent,
};

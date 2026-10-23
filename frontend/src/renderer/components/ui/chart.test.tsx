import { render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Bar, BarChart } from "recharts";
import { ChartContainer, type ChartConfig } from "./chart";

const config = {
	playwright: { label: "Playwright", color: "var(--chart-1)" },
} satisfies ChartConfig;

describe("ChartContainer", () => {
	beforeEach(() => {
		vi.stubGlobal(
			"ResizeObserver",
			class {
				observe() {}
				unobserve() {}
				disconnect() {}
			},
		);
		vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
			width: 320,
			height: 180,
			top: 0,
			left: 0,
			right: 320,
			bottom: 180,
			x: 0,
			y: 0,
			toJSON: () => ({}),
		});
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		vi.restoreAllMocks();
	});

	it("renders a bar chart svg and exposes series colors as --color variables", () => {
		const { container } = render(
			<ChartContainer config={config}>
				<BarChart data={[{ name: "scenario-a", playwright: 12 }]}>
					<Bar dataKey="playwright" fill="var(--color-playwright)" />
				</BarChart>
			</ChartContainer>,
		);

		const wrapper = container.querySelector<HTMLElement>('[data-slot="chart"]');
		expect(wrapper?.querySelector("svg")).not.toBeNull();
		expect(wrapper?.querySelectorAll(".recharts-bar-rectangle")).toHaveLength(1);
		expect(wrapper?.style.getPropertyValue("--color-playwright")).toBe("var(--chart-1)");
	});
});

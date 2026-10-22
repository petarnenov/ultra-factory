import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

vi.mock("../../lib/telemetry", () => ({ captureRendererException: async () => {} }));

import { CoordinatorsErrorBoundary } from "./CoordinatorsErrorBoundary";

let shouldThrow = true;
function Flaky() {
	if (shouldThrow) throw new Error("roster exploded");
	return <p>roster ok</p>;
}

describe("CoordinatorsErrorBoundary", () => {
	test("shows the message inline with Retry and recovers on retry", () => {
		vi.spyOn(console, "error").mockImplementation(() => {});
		render(
			<CoordinatorsErrorBoundary>
				<Flaky />
			</CoordinatorsErrorBoundary>,
		);
		expect(screen.getByRole("alert")).toHaveTextContent("roster exploded");
		shouldThrow = false;
		fireEvent.click(screen.getByRole("button", { name: "Retry" }));
		expect(screen.getByText("roster ok")).toBeInTheDocument();
	});
});

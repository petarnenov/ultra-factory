import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GlobalToast } from "./GlobalToast";
import { useUiStore } from "../stores/ui-store";

describe("GlobalToast", () => {
	afterEach(() => {
		vi.useRealTimers();
		useUiStore.getState().clearGlobalToast();
	});

	it("stacks concurrent notifications and marks errors as alerts", () => {
		const { showGlobalToast } = useUiStore.getState();
		showGlobalToast("First", "Keep this visible");
		showGlobalToast("Second", "Something failed", "error");

		render(<GlobalToast />);

		expect(screen.getAllByRole("status")).toHaveLength(1);
		expect(screen.getAllByRole("alert")).toHaveLength(1);
		expect(screen.getByText("First")).toBeInTheDocument();
		expect(screen.getByText("Second")).toBeInTheDocument();
	});

	it("renders a link that opens in a new tab", () => {
		useUiStore.getState().showGlobalToast("Preview ready", "http://localhost:5173/", {
			link: { label: "Open in new tab", href: "http://localhost:5173/" },
		});

		render(<GlobalToast />);

		const link = screen.getByRole("link", { name: "Open in new tab" });
		expect(link).toHaveAttribute("href", "http://localhost:5173/");
		expect(link).toHaveAttribute("target", "_blank");
		expect(link).toHaveAttribute("rel", "noopener noreferrer");
	});

	it("pauses auto-dismiss while hovered and keeps errors up longer", () => {
		vi.useFakeTimers();
		const store = useUiStore.getState();
		store.showGlobalToast("Plain");
		store.showGlobalToast("Broken", undefined, "error");
		render(<GlobalToast />);

		fireEvent.mouseEnter(screen.getByRole("status"));
		act(() => { vi.advanceTimersByTime(4_000); });
		expect(screen.getByText("Plain")).toBeInTheDocument();
		expect(screen.getByText("Broken")).toBeInTheDocument();

		fireEvent.mouseLeave(screen.getByRole("status"));
		act(() => { vi.advanceTimersByTime(3_600); });
		expect(screen.queryByText("Plain")).not.toBeInTheDocument();
		expect(screen.getByText("Broken")).toBeInTheDocument();
	});

	it("keeps toast keys unique after dismissing the newest toast", () => {
		const store = useUiStore.getState();
		store.showGlobalToast("First");
		store.showGlobalToast("Second");
		store.dismissGlobalToast(2);
		store.showGlobalToast("Third");

		expect(useUiStore.getState().globalToasts.map((toast) => toast.nonce)).toEqual([1, 3]);
	});

	it("offers manual dismissal when requested", () => {
		useUiStore.getState().showGlobalToast("Dismiss me", undefined, { dismissible: true });
		render(<GlobalToast />);

		fireEvent.click(screen.getByRole("button", { name: "Dismiss notification" }));
		expect(screen.queryByText("Dismiss me")).not.toBeInTheDocument();
	});

	it("replaces a keyed toast and restarts its custom dismissal timer", () => {
		vi.useFakeTimers();
		const store = useUiStore.getState();
		store.showGlobalToast("Session stopped", undefined, { dedupeKey: "session:one", durationMs: 5_000 });
		render(<GlobalToast />);

		act(() => vi.advanceTimersByTime(4_000));
		act(() =>
			useUiStore.getState().showGlobalToast("Session stopped", undefined, { dedupeKey: "session:one", durationMs: 5_000 }),
		);
		expect(useUiStore.getState().globalToasts).toHaveLength(1);
		expect(useUiStore.getState().globalToasts[0]?.nonce).toBe(2);

		act(() => vi.advanceTimersByTime(1_000));
		expect(screen.getByText("Session stopped")).toBeInTheDocument();
		act(() => vi.advanceTimersByTime(4_000));
		expect(screen.queryByText("Session stopped")).not.toBeInTheDocument();
	});
});

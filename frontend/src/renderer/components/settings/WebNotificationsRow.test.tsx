import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WebNotificationsRow } from "./WebNotificationsRow";

function installNotificationMock(permission: NotificationPermission, answer: NotificationPermission) {
	const MockNotification = vi.fn() as unknown as typeof Notification & {
		permission: NotificationPermission;
		requestPermission: ReturnType<typeof vi.fn>;
	};
	MockNotification.permission = permission;
	MockNotification.requestPermission = vi.fn(async () => {
		MockNotification.permission = answer;
		return answer;
	});
	vi.stubGlobal("Notification", MockNotification);
	return MockNotification;
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe("WebNotificationsRow", () => {
	it("renders nothing outside the web build", () => {
		installNotificationMock("default", "granted");
		const { container } = render(<WebNotificationsRow webBuild={false} />);
		expect(container).toBeEmptyDOMElement();
	});

	it("does not prompt on mount and asks for permission from the button", async () => {
		const api = installNotificationMock("default", "granted");
		render(<WebNotificationsRow webBuild />);

		expect(api.requestPermission).not.toHaveBeenCalled();
		fireEvent.click(screen.getByRole("button", { name: "Allow" }));

		await waitFor(() => expect(screen.getByRole("button", { name: "Allowed" })).toBeDisabled());
		expect(api.requestPermission).toHaveBeenCalledTimes(1);
	});

	it("explains a blocked permission", () => {
		installNotificationMock("denied", "denied");
		render(<WebNotificationsRow webBuild />);

		expect(screen.getByRole("button", { name: "Blocked" })).toBeDisabled();
		expect(screen.getByText(/Notifications are blocked/)).toBeInTheDocument();
	});
});

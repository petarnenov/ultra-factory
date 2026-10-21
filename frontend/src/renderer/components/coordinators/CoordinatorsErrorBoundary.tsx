import React from "react";
import { useTranslation } from "react-i18next";
import { captureRendererException } from "../../lib/telemetry";
import { Button } from "../ui/button";

type State = { error: Error | null };

class SectionErrorBoundary extends React.Component<
	{ children: React.ReactNode; title: string; retry: string },
	State
> {
	state: State = { error: null };

	static getDerivedStateFromError(error: Error): State {
		return { error };
	}

	componentDidCatch(error: Error) {
		void captureRendererException(error, { source: "react-error-boundary", operation: "coordinators_render" });
	}

	render() {
		const { error } = this.state;
		if (!error) return this.props.children;
		return (
			<div className="flex flex-col gap-2" role="alert">
				<p className="text-sm text-error">{this.props.title}</p>
				<p className="font-mono text-xs text-settings-muted">{error.message}</p>
				<div>
					<Button onClick={() => this.setState({ error: null })} size="sm" type="button" variant="outline">
						{this.props.retry}
					</Button>
				</div>
			</div>
		);
	}
}

/** Keeps a render failure in the Coordinators section inline, with Retry. */
export function CoordinatorsErrorBoundary({ children }: { children: React.ReactNode }) {
	const { t } = useTranslation();
	return (
		<SectionErrorBoundary retry={t("coordinators.errors.retry")} title={t("coordinators.errors.renderFailed")}>
			{children}
		</SectionErrorBoundary>
	);
}

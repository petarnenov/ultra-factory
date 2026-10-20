package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type coordinatorReportOptions struct {
	state         string
	workers       []string
	result        string
	escalate      string
	note          string
	scope         string
	submissionKey string
	requestedBy   string
	json          bool
}

// coordinatorSummaryBody mirrors POST /api/v1/coordinator-summaries.
type coordinatorSummaryBody struct {
	CoordinatorSessionID string   `json:"coordinatorSessionId"`
	SubmissionKey        string   `json:"submissionKey"`
	Scope                string   `json:"scope,omitempty"`
	State                string   `json:"state"`
	WorkerSessionIDs     []string `json:"workerSessionIds,omitempty"`
	Result               string   `json:"result,omitempty"`
	EscalationReason     string   `json:"escalationReason,omitempty"`
	EscalationNote       string   `json:"escalationNote,omitempty"`
}

// coordinatorSummaryDTO mirrors the summary response, also embedded in the
// coordinator roster as the latest summary.
type coordinatorSummaryDTO struct {
	ID               string `json:"id"`
	State            string `json:"state"`
	Result           string `json:"result,omitempty"`
	EscalationReason string `json:"escalationReason,omitempty"`
	Escalation       bool   `json:"escalation"`
	DeliveryState    string `json:"deliveryState"`
}

func newCoordinatorReportCommand(ctx *commandContext) *cobra.Command {
	var opts coordinatorReportOptions
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Send a status summary or escalation to the project root",
		Long: "Send a bounded coordinator summary to the project root. A summary is not a worker report " +
			"and links to at most three workers you own. A blocked summary with --escalate is an escalation " +
			"and reaches the root at once. The caller defaults to AO_SESSION_ID. Reusing --submission-key " +
			"returns the original summary instead of sending another.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.submitCoordinatorSummary(cmd.Context(), cmd, opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.state, "state", "", "Summary state: planning, working, blocked, or done (required)")
	flags.StringArrayVar(&opts.workers, "worker", nil, "Worker session id this summary covers (repeatable, at most 3)")
	flags.StringVar(&opts.result, "result", "", "Result text (at most 2000 characters)")
	flags.StringVar(&opts.escalate, "escalate", "", "Escalation reason; requires --state blocked")
	flags.StringVar(&opts.note, "note", "", "Escalation note (required for --escalate other)")
	flags.StringVar(&opts.scope, "scope", "", "Summary scope (defaults to the coordinator's scope)")
	flags.StringVar(&opts.submissionKey, "submission-key", "", "Idempotency key (defaults to a fresh one)")
	flags.StringVar(&opts.requestedBy, "requested-by", "", "Coordinator session id (defaults to AO_SESSION_ID)")
	flags.BoolVar(&opts.json, "json", false, "Output as JSON")
	return cmd
}

func (c *commandContext) submitCoordinatorSummary(ctx context.Context, cmd *cobra.Command, opts coordinatorReportOptions) error {
	coordinator := strings.TrimSpace(opts.requestedBy)
	if coordinator == "" {
		coordinator = strings.TrimSpace(os.Getenv("AO_SESSION_ID"))
	}
	if coordinator == "" {
		return usageError{errors.New("coordinator session id is required (pass --requested-by or set AO_SESSION_ID)")}
	}
	if strings.TrimSpace(opts.state) == "" {
		return usageError{errors.New("--state is required")}
	}
	key := strings.TrimSpace(opts.submissionKey)
	if key == "" {
		key = "cli-" + uuid.NewString()
	}
	body := coordinatorSummaryBody{
		CoordinatorSessionID: coordinator, SubmissionKey: key, Scope: opts.scope, State: opts.state,
		WorkerSessionIDs: opts.workers, Result: opts.result, EscalationReason: opts.escalate, EscalationNote: opts.note,
	}
	var res coordinatorSummaryDTO
	if err := c.postJSON(ctx, "coordinator-summaries", body, &res); err != nil {
		return err
	}
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), res)
	}
	kind := "summary"
	if res.Escalation {
		kind = "escalation"
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s queued for the root (%s)\n", kind, res.ID, res.DeliveryState)
	return err
}

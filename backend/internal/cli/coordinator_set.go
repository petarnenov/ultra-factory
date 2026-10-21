package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// coordinatorWorkerAgentRequest mirrors the daemon's
// PUT /api/v1/sessions/{sessionId}/coordinator-worker-agent body.
type coordinatorWorkerAgentRequest struct {
	WorkerAgent          agentSelection `json:"workerAgent"`
	RequestedBySessionID string         `json:"requestedBySessionId,omitempty"`
	OperatorInitiated    bool           `json:"operatorInitiated,omitempty"`
	Origin               string         `json:"origin"`
}

type coordinatorSetOptions struct {
	workerAgent string
	requestedBy string
	json        bool
}

func newCoordinatorSetCommand(ctx *commandContext) *cobra.Command {
	var opts coordinatorSetOptions
	cmd := &cobra.Command{
		Use:   "set <coordinator-session-id>",
		Short: "Change a live coordinator's worker agent",
		Long: "Change which agent profiles a live coordinator's new workers run on. The change " +
			"applies only to workers spawned afterwards; running workers keep their agent. The " +
			"project's live root (AO_SESSION_ID) or the operator on its behalf may change it.",
		Args: oneSessionIDArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := normalizeSessionID(args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(opts.workerAgent) == "" {
				return usageError{errors.New("--worker-agent is required (inherit, fixed:<profile-id> or random[:<profile-id>,...])")}
			}
			sel, err := parseAgentSelection("--worker-agent", opts.workerAgent)
			if err != nil {
				return err
			}
			req := coordinatorWorkerAgentRequest{WorkerAgent: *sel, Origin: "cli"}
			req.RequestedBySessionID = strings.TrimSpace(opts.requestedBy)
			if req.RequestedBySessionID == "" && !ctx.targetsRemote() {
				req.RequestedBySessionID = strings.TrimSpace(os.Getenv("AO_SESSION_ID"))
			}
			// Outside an agent session there is no root to speak as: the
			// operator acts on the root's behalf.
			req.OperatorInitiated = req.RequestedBySessionID == ""
			var res sessionOwnershipDTO
			if err := ctx.putJSON(cmd.Context(), "sessions/"+url.PathEscape(id)+"/coordinator-worker-agent", req, &res); err != nil {
				return err
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "coordinator %s worker agent: %s\n", res.SessionID, strings.TrimSpace(opts.workerAgent))
			return err
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.workerAgent, "worker-agent", "", "Worker agent selection: inherit, fixed:<profile-id> or random[:<profile-id>,...] (required)")
	flags.StringVar(&opts.requestedBy, "requested-by", "", "Session id of the live root (defaults to AO_SESSION_ID; empty acts as the operator)")
	flags.BoolVar(&opts.json, "json", false, "Output as JSON")
	addHostFlag(cmd, ctx)
	return cmd
}

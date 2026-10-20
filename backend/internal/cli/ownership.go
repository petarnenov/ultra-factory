package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// sessionOwnerRequest mirrors the daemon's ownership write body. A nil owner
// is root-owned; requestedBySessionId must name the project's live root.
type sessionOwnerRequest struct {
	OwnerSessionID       *string `json:"ownerSessionId"`
	RequestedBySessionID string  `json:"requestedBySessionId"`
}

// sessionOwnershipDTO mirrors GET /api/v1/sessions/{sessionId}/ownership.
type sessionOwnershipDTO struct {
	SessionID        string  `json:"sessionId"`
	ProjectID        string  `json:"projectId,omitempty"`
	Kind             string  `json:"kind"`
	OwnerSessionID   *string `json:"ownerSessionId"`
	OwnerKind        string  `json:"ownerKind,omitempty"`
	OwnerScope       string  `json:"ownerScope,omitempty"`
	CoordinatorScope string  `json:"coordinatorScope,omitempty"`
	RootSessionID    *string `json:"rootSessionId"`
}

// coordinatorRosterDTO mirrors GET /api/v1/projects/{id}/coordinators.
type coordinatorRosterDTO struct {
	ProjectID     string                  `json:"projectId"`
	RootSessionID *string                 `json:"rootSessionId"`
	Coordinators  []coordinatorRosterItem `json:"coordinators"`
	RootWorkerIDs []string                `json:"rootWorkerSessionIds"`
}

type coordinatorRosterItem struct {
	CoordinatorSessionID string   `json:"coordinatorSessionId"`
	CoordinatorScope     string   `json:"coordinatorScope"`
	IsTerminated         bool     `json:"isTerminated"`
	WorkerSessionIDs     []string `json:"workerSessionIds"`
	WorkerCount          int      `json:"workerCount"`
}

// remoteWorkerLinkDTO mirrors the daemon's remote worker link read shape.
type remoteWorkerLinkDTO struct {
	HostID          string  `json:"hostId"`
	HostLabel       string  `json:"hostLabel"`
	RemoteSessionID string  `json:"remoteSessionId"`
	RemoteProjectID string  `json:"remoteProjectId"`
	ProjectID       string  `json:"projectId"`
	DisplayName     string  `json:"displayName,omitempty"`
	OwnerSessionID  *string `json:"ownerSessionId"`
	CreatedAt       string  `json:"createdAt"`
}

type listRemoteWorkerLinksResponse struct {
	Links []remoteWorkerLinkDTO `json:"links"`
}

type sessionAssignOptions struct {
	project     string
	owner       string
	requestedBy string
	json        bool
}

type coordinatorListOptions struct {
	project           string
	includeTerminated bool
	json              bool
}

type remoteWorkerListOptions struct {
	project string
	json    bool
}

type remoteWorkerAssignOptions struct {
	owner       string
	requestedBy string
	json        bool
}

func newSessionOwnerCommand(ctx *commandContext) *cobra.Command {
	var opts sessionOptions
	cmd := &cobra.Command{
		Use:   "owner <id>",
		Short: "Show a session's owner, coordinator scope, and project root",
		Long: "Show the durable ownership relation for one session. A worker with no owner is " +
			"root-owned and resolves to the project's live root. A coordinator is never owned; " +
			"its scope is shown instead.",
		Args: oneSessionIDArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := normalizeSessionID(args[0])
			if err != nil {
				return err
			}
			return ctx.showSessionOwnership(cmd.Context(), cmd, id, opts)
		},
	}
	addSessionProjectFlag(cmd.Flags(), &opts.project, "Project id to scope the lookup")
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output as JSON")
	addHostFlag(cmd, ctx)
	return cmd
}

func newSessionAssignCommand(ctx *commandContext) *cobra.Command {
	var opts sessionAssignOptions
	cmd := &cobra.Command{
		Use:   "assign <worker>",
		Short: "Assign, reassign, or clear a worker's coordinator owner",
		Long: "Assign, reassign, or clear one worker's durable owner. Only the project's live root " +
			"may write ownership; the caller defaults to AO_SESSION_ID. Pass --owner root to return " +
			"the worker to root ownership. A --host target requires an explicit --requested-by " +
			"because a local AO_SESSION_ID is never forwarded to another daemon.",
		Args: oneSessionIDArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := normalizeSessionID(args[0])
			if err != nil {
				return err
			}
			return ctx.assignSessionOwner(cmd.Context(), cmd, id, opts)
		},
	}
	flags := cmd.Flags()
	addSessionProjectFlag(flags, &opts.project, "Project id to scope the lookup")
	flags.StringVar(&opts.owner, "owner", "", "Coordinator session id, or \"root\" to clear the owner (required)")
	flags.StringVar(&opts.requestedBy, "requested-by", "", "Session id of the assigning live root (defaults to AO_SESSION_ID)")
	flags.BoolVar(&opts.json, "json", false, "Output as JSON")
	addHostFlag(cmd, ctx)
	return cmd
}

func newCoordinatorCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coordinator",
		Short: "Inspect a project's coordinator hierarchy",
	}
	cmd.AddCommand(newCoordinatorListCommand(ctx))
	cmd.AddCommand(newCoordinatorReportCommand(ctx))
	return cmd
}

func newCoordinatorListCommand(ctx *commandContext) *cobra.Command {
	var opts coordinatorListOptions
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List coordinators and the workers they own",
		Long: "List a project's coordinators with their scope and owned local workers, plus the " +
			"root and its directly owned workers. Linked remote workers are listed by " +
			"`ao remote-worker ls`.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.listCoordinators(cmd.Context(), cmd, opts)
		},
	}
	flags := cmd.Flags()
	addSessionProjectFlag(flags, &opts.project, "Project id (defaults to AO_PROJECT_ID)")
	flags.BoolVar(&opts.includeTerminated, "include-terminated", false, "Include terminated coordinators and workers")
	flags.BoolVar(&opts.json, "json", false, "Output as JSON")
	addHostFlag(cmd, ctx)
	return cmd
}

func newRemoteWorkerCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote-worker",
		Short: "Inspect and reassign linked remote worker ownership",
	}
	cmd.AddCommand(newRemoteWorkerListCommand(ctx))
	cmd.AddCommand(newRemoteWorkerAssignCommand(ctx))
	return cmd
}

func newRemoteWorkerListCommand(ctx *commandContext) *cobra.Command {
	var opts remoteWorkerListOptions
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List linked remote workers and their local owner",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.listRemoteWorkers(cmd.Context(), cmd, opts)
		},
	}
	flags := cmd.Flags()
	addSessionProjectFlag(flags, &opts.project, "Only links owned by this local project")
	flags.BoolVar(&opts.json, "json", false, "Output as JSON")
	return cmd
}

func newRemoteWorkerAssignCommand(ctx *commandContext) *cobra.Command {
	var opts remoteWorkerAssignOptions
	cmd := &cobra.Command{
		Use:   "assign <host-id> <remote-session-id>",
		Short: "Assign, reassign, or clear a linked remote worker's owner",
		Long: "Assign, reassign, or clear the local coordinator owner of a linked remote worker. " +
			"Only the project's live root may write ownership; the caller defaults to AO_SESSION_ID.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return usageError{err}
			}
			if strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
				return usageError{errors.New("host id and remote session id are required")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.assignRemoteWorkerOwner(cmd.Context(), cmd, args[0], args[1], opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.owner, "owner", "", "Coordinator session id, or \"root\" to clear the owner (required)")
	flags.StringVar(&opts.requestedBy, "requested-by", "", "Session id of the assigning live root (defaults to AO_SESSION_ID)")
	flags.BoolVar(&opts.json, "json", false, "Output as JSON")
	return cmd
}

func (c *commandContext) showSessionOwnership(ctx context.Context, cmd *cobra.Command, id string, opts sessionOptions) error {
	var res sessionOwnershipDTO
	if err := c.getJSON(ctx, "sessions/"+url.PathEscape(id)+"/ownership", &res); err != nil {
		return err
	}
	if opts.project != "" && res.ProjectID != opts.project {
		return usageError{fmt.Errorf("session %s is not in project %s", id, opts.project)}
	}
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), res)
	}
	return writeSessionOwnership(cmd, res)
}

func (c *commandContext) assignSessionOwner(ctx context.Context, cmd *cobra.Command, id string, opts sessionAssignOptions) error {
	req, owner, err := c.ownerRequest(opts.owner, opts.requestedBy)
	if err != nil {
		return err
	}
	if opts.project != "" {
		if _, err := c.fetchScopedSession(ctx, id, opts.project); err != nil {
			return err
		}
	}
	var res sessionResponse
	if err := c.putJSON(ctx, "sessions/"+url.PathEscape(id)+"/owner", req, &res); err != nil {
		return err
	}
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), res)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "session %s owner: %s\n", res.Session.ID, owner)
	return err
}

func (c *commandContext) listCoordinators(ctx context.Context, cmd *cobra.Command, opts coordinatorListOptions) error {
	project := strings.TrimSpace(opts.project)
	if project == "" {
		project = strings.TrimSpace(os.Getenv("AO_PROJECT_ID"))
	}
	if project == "" {
		return usageError{errors.New("project id is required (pass --project or set AO_PROJECT_ID)")}
	}
	params := url.Values{}
	if opts.includeTerminated {
		params.Set("includeTerminated", "true")
	}
	var res coordinatorRosterDTO
	if err := c.getJSON(ctx, apiPath("projects/"+url.PathEscape(project)+"/coordinators", params), &res); err != nil {
		return err
	}
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), res)
	}
	return writeCoordinatorRoster(cmd, res)
}

func (c *commandContext) listRemoteWorkers(ctx context.Context, cmd *cobra.Command, opts remoteWorkerListOptions) error {
	params := url.Values{}
	if opts.project != "" {
		params.Set("project", opts.project)
	}
	var res listRemoteWorkerLinksResponse
	if err := c.getJSON(ctx, apiPath("remote-worker-links", params), &res); err != nil {
		return err
	}
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), res)
	}
	return writeRemoteWorkerList(cmd, res)
}

func (c *commandContext) assignRemoteWorkerOwner(ctx context.Context, cmd *cobra.Command, hostID, remoteSessionID string, opts remoteWorkerAssignOptions) error {
	req, owner, err := c.ownerRequest(opts.owner, opts.requestedBy)
	if err != nil {
		return err
	}
	path := "remote-worker-links/" + url.PathEscape(strings.TrimSpace(hostID)) + "/" +
		url.PathEscape(strings.TrimSpace(remoteSessionID)) + "/owner"
	var res remoteWorkerLinkDTO
	if err := c.putJSON(ctx, path, req, &res); err != nil {
		return err
	}
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), res)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "remote worker %s/%s owner: %s\n", res.HostID, res.RemoteSessionID, owner)
	return err
}

// ownerRequest builds the shared ownership write body and the label to print.
func (c *commandContext) ownerRequest(rawOwner, rawRequestedBy string) (sessionOwnerRequest, string, error) {
	owner := strings.TrimSpace(rawOwner)
	if owner == "" {
		return sessionOwnerRequest{}, "", usageError{errors.New("--owner is required (a coordinator session id or \"root\")")}
	}
	requestedBy := strings.TrimSpace(rawRequestedBy)
	if requestedBy == "" && !c.targetsRemote() {
		requestedBy = strings.TrimSpace(os.Getenv("AO_SESSION_ID"))
	}
	if requestedBy == "" {
		return sessionOwnerRequest{}, "", usageError{errors.New("requested-by session id is required (pass --requested-by or set AO_SESSION_ID)")}
	}
	req := sessionOwnerRequest{RequestedBySessionID: requestedBy}
	label := owner
	if strings.EqualFold(owner, "root") {
		return req, "root", nil
	}
	req.OwnerSessionID = &owner
	return req, label, nil
}

func writeSessionOwnership(cmd *cobra.Command, res sessionOwnershipDTO) error {
	out := cmd.OutOrStdout()
	fields := [][2]string{
		{"session", res.SessionID},
		{"project", res.ProjectID},
		{"role", res.Kind},
		{"owner", ownershipLabel(res)},
		{"coordinator scope", res.CoordinatorScope},
		{"root", ownershipID(res.RootSessionID)},
	}
	for _, field := range fields {
		if field[1] == "" || field[1] == "-" {
			continue
		}
		if _, err := fmt.Fprintf(out, "%s: %s\n", field[0], field[1]); err != nil {
			return err
		}
	}
	return nil
}

func ownershipLabel(res sessionOwnershipDTO) string {
	if res.OwnerSessionID == nil {
		if res.Kind == string(domain.KindWorker) {
			return "root"
		}
		return ""
	}
	label := *res.OwnerSessionID
	if res.OwnerKind == "" && res.OwnerScope == "" {
		return label
	}
	detail := res.OwnerKind
	if res.OwnerScope != "" {
		detail += ", scope: " + res.OwnerScope
	}
	return label + " (" + strings.Trim(detail, ", ") + ")"
}

func ownershipID(id *string) string {
	if id == nil {
		return "-"
	}
	return *id
}

func writeCoordinatorRoster(cmd *cobra.Command, res coordinatorRosterDTO) error {
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "%s\n", res.ProjectID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "  root: %s\n", ownershipID(res.RootSessionID)); err != nil {
		return err
	}
	if len(res.Coordinators) == 0 {
		if _, err := fmt.Fprintln(out, "  (no coordinators)"); err != nil {
			return err
		}
	}
	for _, coordinator := range res.Coordinators {
		status := ""
		if coordinator.IsTerminated {
			status = " terminated"
		}
		if _, err := fmt.Fprintf(out, "  coordinator %s%s (scope: %s)  workers: %d\n",
			coordinator.CoordinatorSessionID, status, coordinator.CoordinatorScope, coordinator.WorkerCount); err != nil {
			return err
		}
		for _, worker := range coordinator.WorkerSessionIDs {
			if _, err := fmt.Fprintf(out, "    %s\n", worker); err != nil {
				return err
			}
		}
	}
	if len(res.RootWorkerIDs) > 0 {
		if _, err := fmt.Fprintf(out, "  root-owned workers: %d\n", len(res.RootWorkerIDs)); err != nil {
			return err
		}
		for _, worker := range res.RootWorkerIDs {
			if _, err := fmt.Fprintf(out, "    %s\n", worker); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeRemoteWorkerList(cmd *cobra.Command, res listRemoteWorkerLinksResponse) error {
	out := cmd.OutOrStdout()
	if len(res.Links) == 0 {
		_, err := fmt.Fprintln(out, "(no remote workers)")
		return err
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "HOST\tREMOTE SESSION\tPROJECT\tOWNER\tNAME"); err != nil {
		return err
	}
	for _, link := range res.Links {
		owner := "root"
		if link.OwnerSessionID != nil {
			owner = *link.OwnerSessionID
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n",
			link.HostID, link.RemoteSessionID, link.ProjectID, owner, emptyDash(link.DisplayName)); err != nil {
			return err
		}
	}
	return table.Flush()
}

// sessionOwnerColumn renders a session list owner cell.
func sessionOwnerColumn(sess sessionDTO) string {
	if sess.Kind != string(domain.KindWorker) {
		return "-"
	}
	if sess.OwnerSessionID == nil {
		return "root"
	}
	return *sess.OwnerSessionID
}

// sessionOwnerDetails renders the session get owner line; empty omits it.
func sessionOwnerDetails(sess sessionDTO) string {
	if sess.Kind == string(domain.KindWorker) {
		if sess.OwnerSessionID == nil {
			return "root"
		}
		return *sess.OwnerSessionID
	}
	return ""
}

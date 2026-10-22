package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
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
	// WorkerAgent is a coordinator's stored worker agent selection.
	WorkerAgent *agentSelection `json:"workerAgent,omitempty"`
	// WorkerLimit is a coordinator's stored limit (nil: project value);
	// EffectivePlacementLimit is what new spawns are checked against.
	WorkerLimit             *int                  `json:"workerLimit"`
	EffectiveWorkerLimit    *int                  `json:"effectiveWorkerLimit,omitempty"`
	EffectivePlacementLimit *int                  `json:"effectivePlacementLimit,omitempty"`
	HostSlots               []coordinatorHostSlot `json:"hostSlots,omitempty"`
}

// coordinatorHostSlot mirrors one stored host slot and its live usage.
type coordinatorHostSlot struct {
	HostID string `json:"hostId"`
	Slots  int    `json:"slots"`
	Used   int    `json:"used"`
}

// coordinatorRosterDTO mirrors GET /api/v1/projects/{id}/coordinators.
type coordinatorRosterDTO struct {
	ProjectID     string                    `json:"projectId"`
	RootSessionID *string                   `json:"rootSessionId"`
	Coordinators  []coordinatorRosterItem   `json:"coordinators"`
	RootWorkerIDs []string                  `json:"rootWorkerSessionIds"`
	RootWorkers   []coordinatorRosterWorker `json:"rootWorkers"`
}

type coordinatorRosterItem struct {
	CoordinatorSessionID string   `json:"coordinatorSessionId"`
	CoordinatorScope     string   `json:"coordinatorScope"`
	IsTerminated         bool     `json:"isTerminated"`
	WorkerSessionIDs     []string `json:"workerSessionIds"`
	WorkerCount          int      `json:"workerCount"`
	// NeedsRecovery flags a terminated coordinator that still owns live workers.
	NeedsRecovery bool                   `json:"needsRecovery"`
	LatestSummary *coordinatorSummaryDTO `json:"latestSummary"`
	// Limits, host slots and the agent selection. Workers pair each live
	// worker with the profile that picked its agent.
	WorkerLimit             *int                      `json:"workerLimit"`
	EffectiveWorkerLimit    int                       `json:"effectiveWorkerLimit"`
	ActiveWorkerCount       int                       `json:"activeWorkerCount"`
	EffectivePlacementLimit int                       `json:"effectivePlacementLimit"`
	HostSlots               []coordinatorHostSlot     `json:"hostSlots"`
	AgentProfileID          *string                   `json:"agentProfileId"`
	WorkerAgent             *agentSelection           `json:"workerAgent,omitempty"`
	Workers                 []coordinatorRosterWorker `json:"workers"`
}

// coordinatorRosterWorker is one live worker and the profile that chose its
// agent, when a profile did.
type coordinatorRosterWorker struct {
	SessionID      string  `json:"sessionId"`
	AgentProfileID *string `json:"agentProfileId"`
	Remote         bool    `json:"remote"`
	HostID         string  `json:"hostId"`
	HostLabel      string  `json:"hostLabel"`
}

// sessionTreeOutput is the --json body of `session ls --tree`.
type sessionTreeOutput struct {
	Data []coordinatorRosterDTO `json:"data"`
}

// sessionDetailOutput is the --json body of `session get`; a coordinator
// also carries its roster entry.
type sessionDetailOutput struct {
	Session     sessionDTO             `json:"session"`
	Coordinator *coordinatorRosterItem `json:"coordinator,omitempty"`
}

// remoteWorkerLinkDTO mirrors the daemon's remote worker link read shape.
type remoteWorkerLinkDTO struct {
	HostID          string  `json:"hostId"`
	HostLabel       string  `json:"hostLabel"`
	RemoteSessionID string  `json:"remoteSessionId"`
	RemoteProjectID string  `json:"remoteProjectId"`
	ProjectID       string  `json:"projectId"`
	LocalProjectID  string  `json:"localProjectId"`
	DisplayName     string  `json:"displayName,omitempty"`
	OwnerSessionID  *string `json:"ownerSessionId"`
	Source          string  `json:"source"`
	CreatedAt       string  `json:"createdAt"`
	State           string  `json:"state"`
	LastSeenAt      *string `json:"lastSeenAt,omitempty"`
}

// remoteWorkerSyncResponse mirrors POST /api/v1/remote-worker-links/sync.
type remoteWorkerSyncResponse struct {
	Hosts []remoteWorkerHostSyncDTO `json:"hosts"`
}

type remoteWorkerHostSyncDTO struct {
	HostID    string `json:"hostId"`
	HostLabel string `json:"hostLabel"`
	Linked    int    `json:"linked"`
	Skipped   int    `json:"skipped"`
	Blocked   string `json:"blocked,omitempty"`
	Error     string `json:"error,omitempty"`
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

type remoteWorkerSyncOptions struct {
	json bool
}

type remoteWorkerAssignOptions struct {
	owner       string
	requestedBy string
	json        bool
}

type remoteWorkerRemoveOptions struct {
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
	cmd.AddCommand(newCoordinatorSetCommand(ctx))
	cmd.AddCommand(newCoordinatorSetLimitsCommand(ctx))
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
	cmd.AddCommand(newRemoteWorkerSyncCommand(ctx))
	cmd.AddCommand(newRemoteWorkerRemoveCommand(ctx))
	return cmd
}

func newRemoteWorkerRemoveCommand(ctx *commandContext) *cobra.Command {
	var opts remoteWorkerRemoveOptions
	cmd := &cobra.Command{
		Use:   "rm <host-id> <remote-session-id>",
		Short: "Drop a linked remote worker by hand",
		Long: "Remove the link of a remote worker so it no longer counts toward the project's worker cap " +
			"or gets its reports pulled. Discovery does not link it again while the worker is terminated. " +
			"Only the project's live root may remove a link; the caller defaults to AO_SESSION_ID.",
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
			return ctx.removeRemoteWorker(cmd.Context(), cmd, args[0], args[1], opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.requestedBy, "requested-by", "", "Session id of the removing live root (defaults to AO_SESSION_ID)")
	flags.BoolVar(&opts.json, "json", false, "Output as JSON")
	return cmd
}

func newRemoteWorkerSyncCommand(ctx *commandContext) *cobra.Command {
	var opts remoteWorkerSyncOptions
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Link live remote workers on saved hosts now",
		Long: "Run one discovery pass: link every live worker on a saved host that has no link yet, " +
			"including workers removed by hand with rm. " +
			"Each host prints its linked and skipped counts; a blocked or offline host is reported, not fatal.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.syncRemoteWorkers(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output as JSON")
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
	res, err := c.fetchCoordinatorRoster(ctx, project, opts.includeTerminated)
	if err != nil {
		return err
	}
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), res)
	}
	return writeCoordinatorRoster(cmd, res, nil, c.savedHostLabels())
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

func (c *commandContext) syncRemoteWorkers(ctx context.Context, cmd *cobra.Command, opts remoteWorkerSyncOptions) error {
	var res remoteWorkerSyncResponse
	if err := c.postJSON(ctx, "remote-worker-links/sync", nil, &res); err != nil {
		return err
	}
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), res)
	}
	return writeRemoteWorkerSync(cmd, res)
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
func (c *commandContext) removeRemoteWorker(ctx context.Context, cmd *cobra.Command, hostID, remoteSessionID string, opts remoteWorkerRemoveOptions) error {
	requestedBy := strings.TrimSpace(opts.requestedBy)
	if requestedBy == "" {
		requestedBy = strings.TrimSpace(os.Getenv("AO_SESSION_ID"))
	}
	if requestedBy == "" {
		return usageError{errors.New("requested-by session id is required (pass --requested-by or set AO_SESSION_ID)")}
	}
	path := "remote-worker-links/" + url.PathEscape(strings.TrimSpace(hostID)) + "/" +
		url.PathEscape(strings.TrimSpace(remoteSessionID))
	params := url.Values{"requestedBySessionId": {requestedBy}}
	var res remoteWorkerLinkDTO
	if err := c.deleteJSON(ctx, apiPath(path, params), &res); err != nil {
		return err
	}
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), res)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "remote worker %s/%s removed\n", res.HostID, res.RemoteSessionID)
	return err
}

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

// fetchCoordinatorRoster reads one project's ownership hierarchy.
func (c *commandContext) fetchCoordinatorRoster(ctx context.Context, project string, includeTerminated bool) (coordinatorRosterDTO, error) {
	params := url.Values{}
	if includeTerminated {
		params.Set("includeTerminated", "true")
	}
	var res coordinatorRosterDTO
	err := c.getJSON(ctx, apiPath("projects/"+url.PathEscape(project)+"/coordinators", params), &res)
	return res, err
}

// listSessionTree renders `session ls --tree` from the daemon roster of every
// project in the listing, labelling workers with the facts already listed.
func (c *commandContext) listSessionTree(ctx context.Context, cmd *cobra.Command, sessions []sessionDTO, opts sessionListOptions) error {
	byID := make(map[string]sessionDTO, len(sessions))
	projects := make([]string, 0)
	seen := make(map[string]bool)
	for _, sess := range sessions {
		byID[sess.ID] = sess
		if sess.ProjectID != "" && !seen[sess.ProjectID] {
			seen[sess.ProjectID] = true
			projects = append(projects, sess.ProjectID)
		}
	}
	sort.Strings(projects)
	rosters := make([]coordinatorRosterDTO, 0, len(projects))
	for _, project := range projects {
		roster, err := c.fetchCoordinatorRoster(ctx, project, opts.includeTerminated)
		if err != nil {
			return err
		}
		rosters = append(rosters, roster)
	}
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), sessionTreeOutput{Data: rosters})
	}
	if len(rosters) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "(no active sessions)")
		return err
	}
	for _, roster := range rosters {
		if err := writeCoordinatorRoster(cmd, roster, byID, c.savedHostLabels()); err != nil {
			return err
		}
	}
	return nil
}

// writeCoordinatorRoster prints the ownership tree. known adds each worker's
// role and status when set; labels names host slots.
func writeCoordinatorRoster(cmd *cobra.Command, res coordinatorRosterDTO, known map[string]sessionDTO, labels map[string]string) error {
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
	workerLine := func(worker coordinatorRosterWorker, profile string) error {
		label := worker.SessionID
		facts := workerFacts(known, worker.SessionID)
		if worker.Remote {
			host := worker.HostLabel
			if host == "" {
				host = worker.HostID
			}
			label += " on " + host
			facts = ""
		}
		_, err := fmt.Fprintf(out, "    %s%s%s\n", label, facts, profileFact(profile))
		return err
	}
	for _, coordinator := range res.Coordinators {
		status := ""
		if coordinator.IsTerminated {
			status = " terminated"
		}
		if coordinator.NeedsRecovery {
			status += " NEEDS RECOVERY"
		}
		if _, err := fmt.Fprintf(out, "  coordinator %s%s (scope: %s)  workers: %d\n",
			coordinator.CoordinatorSessionID, status, coordinator.CoordinatorScope, coordinator.WorkerCount); err != nil {
			return err
		}
		if err := writeCoordinatorLimits(out, "    ", coordinator, labels); err != nil {
			return err
		}
		if err := writeLatestSummary(out, "    ", coordinator.LatestSummary); err != nil {
			return err
		}
		profiles := workerProfiles(coordinator.Workers)
		rows := coordinator.Workers
		if len(rows) == 0 {
			rows = make([]coordinatorRosterWorker, len(coordinator.WorkerSessionIDs))
			for i, id := range coordinator.WorkerSessionIDs {
				rows[i] = coordinatorRosterWorker{SessionID: id}
			}
		}
		for _, worker := range rows {
			if err := workerLine(worker, profiles[workerKey(worker)]); err != nil {
				return err
			}
		}
	}
	rootWorkers := res.RootWorkers
	if len(rootWorkers) == 0 {
		rootWorkers = make([]coordinatorRosterWorker, len(res.RootWorkerIDs))
		for i, id := range res.RootWorkerIDs {
			rootWorkers[i] = coordinatorRosterWorker{SessionID: id}
		}
	}
	if len(rootWorkers) > 0 {
		label := "root-owned workers"
		if res.RootSessionID == nil {
			label = "unassigned workers (no live root)"
		}
		if _, err := fmt.Fprintf(out, "  %s: %d\n", label, len(rootWorkers)); err != nil {
			return err
		}
		for _, worker := range rootWorkers {
			if err := workerLine(worker, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeCoordinatorLimits prints a coordinator's limit and host slots when the
// daemon sent them, then its worker agent selection.
func writeCoordinatorLimits(out io.Writer, indent string, entry coordinatorRosterItem, labels map[string]string) error {
	if entry.EffectivePlacementLimit > 0 {
		if _, err := fmt.Fprintf(out, "%slimit: %d\n", indent, entry.EffectivePlacementLimit); err != nil {
			return err
		}
	}
	if len(entry.HostSlots) > 0 {
		if _, err := fmt.Fprintf(out, "%shosts: %s\n", indent, hostSlotText(entry.HostSlots, labels)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(out, "%sworker agent: %s\n", indent, formatAgentSelection(entry.WorkerAgent))
	return err
}

// workerProfiles maps each worker to the profile that picked its agent.
func workerProfiles(workers []coordinatorRosterWorker) map[string]string {
	profiles := make(map[string]string, len(workers))
	for _, worker := range workers {
		if worker.AgentProfileID != nil {
			profiles[workerKey(worker)] = *worker.AgentProfileID
		}
	}
	return profiles
}

func workerKey(worker coordinatorRosterWorker) string {
	return worker.HostID + "\x00" + worker.SessionID
}

// profileFact names the profile that picked a worker's agent, if one did.
func profileFact(profile string) string {
	if profile == "" {
		return ""
	}
	return "  (profile: " + profile + ")"
}

func workerFacts(known map[string]sessionDTO, id string) string {
	sess, ok := known[id]
	if !ok {
		return ""
	}
	facts := emptyDash(sess.Status)
	if sess.IsTerminated {
		facts += ", terminated"
	}
	return "  (" + facts + ")"
}

func writeLatestSummary(out io.Writer, indent string, summary *coordinatorSummaryDTO) error {
	if summary == nil {
		return nil
	}
	line := summary.State
	if summary.Escalation {
		line += ", escalation: " + summary.EscalationReason
	}
	if summary.Result != "" {
		line += " - " + summary.Result
	}
	_, err := fmt.Fprintf(out, "%slatest summary: %s\n", indent, line)
	return err
}

func writeRemoteWorkerList(cmd *cobra.Command, res listRemoteWorkerLinksResponse) error {
	out := cmd.OutOrStdout()
	if len(res.Links) == 0 {
		_, err := fmt.Fprintln(out, "(no remote workers)")
		return err
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "HOST\tREMOTE SESSION\tPROJECT\tOWNER\tSTATE\tSOURCE\tNAME"); err != nil {
		return err
	}
	for _, link := range res.Links {
		owner := "root"
		if link.OwnerSessionID != nil {
			owner = *link.OwnerSessionID
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			link.HostID, link.RemoteSessionID, link.ProjectID, owner, emptyDash(link.State),
			emptyDash(link.Source), emptyDash(link.DisplayName)); err != nil {
			return err
		}
	}
	return table.Flush()
}

// writeRemoteWorkerSync prints one line per saved host.
func writeRemoteWorkerSync(cmd *cobra.Command, res remoteWorkerSyncResponse) error {
	out := cmd.OutOrStdout()
	if len(res.Hosts) == 0 {
		_, err := fmt.Fprintln(out, "(no saved remote hosts)")
		return err
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, host := range res.Hosts {
		label := host.HostLabel
		if label == "" {
			label = host.HostID
		}
		status := fmt.Sprintf("linked %d, skipped %d", host.Linked, host.Skipped)
		if host.Blocked != "" {
			status += ", blocked: " + host.Blocked
		}
		if host.Error != "" {
			status += ", error: " + host.Error
		}
		if _, err := fmt.Fprintf(table, "%s\t%s\n", label, status); err != nil {
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

// writeCoordinatorDetails prints a coordinator's workers, recovery flag, and
// latest summary under `session get`; a nil entry prints nothing.
func writeCoordinatorDetails(out io.Writer, entry *coordinatorRosterItem, labels map[string]string) error {
	if entry == nil {
		return nil
	}
	if entry.NeedsRecovery {
		if _, err := fmt.Fprintln(out, "needs recovery: terminated coordinator still owns live workers"); err != nil {
			return err
		}
	}
	profiles := workerProfiles(entry.Workers)
	workers := make([]string, 0, len(entry.Workers)+len(entry.WorkerSessionIDs))
	rows := entry.Workers
	if len(rows) == 0 {
		rows = make([]coordinatorRosterWorker, len(entry.WorkerSessionIDs))
		for i, id := range entry.WorkerSessionIDs {
			rows[i] = coordinatorRosterWorker{SessionID: id}
		}
	}
	for _, worker := range rows {
		label := worker.SessionID
		if worker.Remote {
			host := worker.HostLabel
			if host == "" {
				host = worker.HostID
			}
			label += " on " + host
		} else {
			label += profileFact(profiles[workerKey(worker)])
		}
		workers = append(workers, label)
	}
	if _, err := fmt.Fprintf(out, "workers: %s\n", joinOrDash(workers)); err != nil {
		return err
	}
	if err := writeCoordinatorLimits(out, "", *entry, labels); err != nil {
		return err
	}
	return writeLatestSummary(out, "", entry.LatestSummary)
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ", ")
}

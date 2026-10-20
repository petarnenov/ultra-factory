package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// maxDisplayNameLen caps the sidebar label set by `--name`. Mirrored by the
// daemon's spawn handler so a direct API call is held to the same limit.
const maxDisplayNameLen = 100

const maxCoordinatorScopeLen = 200

type spawnOptions struct {
	project            string
	standalone         bool
	harness            string
	kind               string
	mode               string
	branch             string
	prompt             string
	issue              string
	name               string
	model              string
	effort             string
	output             string
	claimPR            string
	noTakeover         bool
	skipAgentCheck     bool
	trackerProvider    string
	terminateOnPRMerge bool
	scope              string
	workerAgent        string
	coordinatorAgent   string
}

// spawnRequest mirrors the daemon's SpawnSessionRequest body for
// POST /api/v1/sessions. The CLI keeps its own copy so it need not import httpd.
type spawnRequest struct {
	ProjectID       string `json:"projectId,omitempty"`
	IssueID         string `json:"issueId,omitempty"`
	ParentSessionID string `json:"parentSessionId,omitempty"`
	TrackerProvider string `json:"trackerProvider,omitempty"`
	Kind            string `json:"kind,omitempty"`
	Scope           string `json:"scope,omitempty"`
	// WorkerAgent and CoordinatorAgent are the agent selections of a
	// coordinator spawn.
	WorkerAgent      *agentSelection `json:"workerAgent,omitempty"`
	CoordinatorAgent *agentSelection `json:"coordinatorAgent,omitempty"`
	Mode             string          `json:"mode,omitempty"`
	Harness          string          `json:"harness,omitempty"`
	// HarnessExplicit distinguishes a caller-named harness from one resolved
	// from Project Settings or the machine default.
	HarnessExplicit    *bool  `json:"harnessExplicit,omitempty"`
	Branch             string `json:"branch,omitempty"`
	Prompt             string `json:"prompt,omitempty"`
	Model              string `json:"model,omitempty"`
	Effort             string `json:"effort,omitempty"`
	RequestedOutput    string `json:"requestedOutput,omitempty"`
	DisplayName        string `json:"displayName"`
	RemoteSpawn        bool   `json:"remoteSpawn,omitempty"`
	AgentProfileID     string `json:"agentProfileId,omitempty"`
	TerminateOnPRMerge *bool  `json:"terminateOnPrMerge,omitempty"`
}

type spawnResult struct {
	Session struct {
		ID          string `json:"id"`
		ProjectID   string `json:"projectId"`
		Status      string `json:"status"`
		DisplayName string `json:"displayName"`
	} `json:"session"`
	PromptBytes       int `json:"promptBytes,omitempty"`
	SystemPromptBytes int `json:"systemPromptBytes,omitempty"`
}

// remoteWorkerLinkRequest mirrors the daemon's CreateRemoteWorkerLinkRequest
// body for POST /api/v1/remote-worker-links.
type remoteWorkerLinkRequest struct {
	ParentSessionID string `json:"parentSessionId"`
	HostID          string `json:"hostId"`
	RemoteSessionID string `json:"remoteSessionId"`
	RemoteProjectID string `json:"remoteProjectId"`
	DisplayName     string `json:"displayName,omitempty"`
}

func newSpawnCommand(ctx *commandContext) *cobra.Command {
	var opts spawnOptions
	cmd := &cobra.Command{
		Use:   "spawn",
		Short: "Spawn an agent session",
		Long: "Spawn an agent session in a registered project, or a standalone worker with --standalone.\n\n" +
			"The session runs the chosen agent in a\n" +
			"fresh isolated workspace. Git projects use worktrees; standalone agents use an AO-managed plain directory.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.standalone && strings.TrimSpace(opts.project) != "" {
				return usageError{fmt.Errorf("--standalone and --project cannot be used together")}
			}
			if opts.noTakeover && opts.claimPR == "" {
				return usageError{fmt.Errorf("--no-takeover requires --claim-pr")}
			}
			name := strings.TrimSpace(opts.name)
			if name == "" {
				return usageError{fmt.Errorf("--name is required")}
			}
			if utf8.RuneCountInString(name) > maxDisplayNameLen {
				return usageError{fmt.Errorf("--name must be %d characters or fewer", maxDisplayNameLen)}
			}

			// Rejected here rather than forwarded, so a typo exits 2 as a usage
			// error instead of reaching the daemon as an unsupported mode.
			if opts.mode != "" && opts.mode != "chat" && opts.mode != "tui" {
				return usageError{fmt.Errorf(`--mode must be "chat" or "tui"`)}
			}
			if opts.kind != "" && opts.kind != "worker" && opts.kind != "orchestrator" && opts.kind != "coordinator" {
				return usageError{fmt.Errorf(`--kind must be "worker", "orchestrator", or "coordinator"`)}
			}
			if err := validateCoordinatorSpawn(opts, ctx.targetsRemote()); err != nil {
				return err
			}
			if opts.output != "" && opts.output != "artifact" {
				return usageError{fmt.Errorf(`--output must be "artifact"`)}
			}
			if opts.output != "" && opts.kind != "" && opts.kind != "worker" {
				return usageError{fmt.Errorf("--output is supported for worker sessions only")}
			}
			if opts.standalone {
				if opts.kind == "orchestrator" || opts.kind == "coordinator" {
					return usageError{fmt.Errorf("standalone sessions must be workers")}
				}
				if strings.TrimSpace(opts.branch) != "" || strings.TrimSpace(opts.issue) != "" || strings.TrimSpace(opts.claimPR) != "" {
					return usageError{fmt.Errorf("standalone sessions do not support --branch, --issue, or --claim-pr")}
				}
				if strings.TrimSpace(opts.harness) == "" {
					return usageError{fmt.Errorf("--agent is required with --standalone")}
				}
				opts.kind = "worker"
			}

			tp := strings.TrimSpace(opts.trackerProvider)
			if tp == "" {
				tp = "github"
			}
			if tp != "github" && tp != "gitlab" {
				return usageError{fmt.Errorf(`--tracker-provider must be "github" or "gitlab"`)}
			}
			opts.trackerProvider = tp

			var project projectDetails
			var err error
			if !opts.standalone {
				project, err = ctx.resolveSpawnProject(cmd.Context(), opts.project)
				if err != nil {
					return err
				}
				opts.project = project.ID
			}

			harness, err := resolveSpawnHarness(strings.TrimSpace(opts.harness), opts.kind, project)
			if err != nil {
				return err
			}
			explicitHarness := strings.TrimSpace(opts.harness)
			opts.harness = harness

			if isScratchProject(project) {
				if strings.TrimSpace(opts.branch) != "" {
					return usageError{fmt.Errorf("scratch projects do not support --branch")}
				}
				if strings.TrimSpace(opts.claimPR) != "" {
					return usageError{fmt.Errorf("scratch projects do not support --claim-pr")}
				}
			}

			// With agent profiles the daemon picks the agent for a coordinator's
			// or the root's spawn, so the CLI default is not what will launch.
			profilesPick := explicitHarness == "" && project.Config != nil && len(project.Config.Coordinators.AgentProfiles) > 0
			if !opts.skipAgentCheck && !profilesPick {
				if err := ctx.preflightSpawnAgentAuth(cmd.Context(), cmd, opts.harness); err != nil {
					return err
				}
			}
			claimRef := ""
			if opts.claimPR != "" {
				claimRef, err = ctx.resolvePRRef(cmd.Context(), opts.claimPR, project)
				if err != nil {
					return err
				}
			}
			// Session IDs are per daemon: a local orchestrator's ID would name an
			// unrelated session (or none) on a remote host. The local parent is
			// instead linked to the remote worker below, on this machine's daemon.
			localParentID := strings.TrimSpace(os.Getenv("AO_SESSION_ID"))
			parentSessionID := localParentID
			if ctx.targetsRemote() {
				parentSessionID = ""
			}
			workerAgent, coordinatorAgent, err := coordinatorAgentSelections(opts)
			if err != nil {
				return err
			}
			var terminateOnPRMerge *bool
			if cmd.Flags().Changed("terminate-on-pr-merge") {
				value := opts.terminateOnPRMerge
				terminateOnPRMerge = &value
			}
			req := spawnRequest{
				ProjectID:          opts.project,
				IssueID:            opts.issue,
				ParentSessionID:    parentSessionID,
				TrackerProvider:    opts.trackerProvider,
				Kind:               opts.kind,
				Scope:              opts.scope,
				WorkerAgent:        workerAgent,
				CoordinatorAgent:   coordinatorAgent,
				Harness:            opts.harness,
				HarnessExplicit:    spawnBoolPtr(explicitHarness != ""),
				Mode:               opts.mode,
				Branch:             opts.branch,
				Prompt:             opts.prompt,
				Model:              strings.TrimSpace(opts.model),
				Effort:             strings.TrimSpace(opts.effort),
				RequestedOutput:    opts.output,
				DisplayName:        name,
				RemoteSpawn:        ctx.targetsRemote() && opts.kind != "orchestrator",
				TerminateOnPRMerge: terminateOnPRMerge,
			}
			if ctx.remote != nil && localParentID != "" && !opts.standalone {
				if err := ctx.checkRemoteWorkerCapacity(cmd.Context(), localParentID); err != nil {
					return err
				}
				agent, err := ctx.checkRemoteWorkerAgent(cmd.Context(), localParentID, &req, explicitHarness != "")
				if err != nil {
					return err
				}
				if agent != nil {
					req.Harness, req.Model, req.Effort = agent.Harness, agent.Model, agent.Effort
					req.HarnessExplicit, req.AgentProfileID = spawnBoolPtr(true), agent.ProfileID
				}
			}
			var res spawnResult
			if err := ctx.postJSON(cmd.Context(), "sessions", req, &res); err != nil {
				return err
			}
			if strings.TrimSpace(res.Session.ID) == "" {
				return fmt.Errorf("daemon returned empty session id for spawn")
			}
			claimed := ""
			var claim claimPRResponse
			if opts.claimPR != "" {
				if err := ctx.postJSON(cmd.Context(), "sessions/"+url.PathEscape(res.Session.ID)+"/pr/claim", claimPRRequest{PR: claimRef, AllowTakeover: !opts.noTakeover}, &claim); err != nil {
					if killErr := ctx.rollbackSpawnedSession(cmd.Context(), res.Session.ID); killErr != nil {
						return fmt.Errorf("failed to claim PR %s: %w; rollback of session %s failed: %w", opts.claimPR, err, res.Session.ID, killErr)
					}
					return fmt.Errorf("failed to claim PR %s: %w; rolled back session %s", opts.claimPR, err, res.Session.ID)
				}
				if len(claim.PRs) > 0 {
					claimed = claim.PRs[0].URL
				}
			}
			if ctx.remote != nil && localParentID != "" && res.Session.ProjectID == "" {
				// A projectless worker has nothing to link to a local project.
				if _, werr := fmt.Fprintf(cmd.ErrOrStderr(), "warning: reports from %s will not reach this machine's orchestrator: the worker has no project\n", res.Session.ID); werr != nil {
					return werr
				}
			} else if ctx.remote != nil && localParentID != "" {
				blocked, err := ctx.linkRemoteWorker(cmd.Context(), localParentID, res)
				if err != nil {
					return ctx.failRemoteLink(cmd.Context(), res.Session.ID, err)
				}
				if blocked != "" {
					if _, werr := fmt.Fprintf(cmd.ErrOrStderr(), "warning: reports from %s will not reach this machine's orchestrator: %s\n", res.Session.ID, blocked); werr != nil {
						return werr
					}
				}
			}
			out := cmd.OutOrStdout()
			claimLabel := ""
			if claimed != "" {
				claimLabel = fmt.Sprintf(" (claimed %s)", claimed)
			}
			promptSize := ""
			if res.PromptBytes > 0 || res.SystemPromptBytes > 0 {
				promptSize = fmt.Sprintf(" [prompt %d B, system %d B]", res.PromptBytes, res.SystemPromptBytes)
			}
			displayName := res.Session.DisplayName
			if displayName == "" {
				displayName = name
			}
			hostLabel := ""
			if ctx.remote != nil {
				hostLabel = fmt.Sprintf(" on host %q", ctx.remote.label)
			}
			if _, err := fmt.Fprintf(out, "spawned session %s %q (%s)%s%s%s\n", res.Session.ID, displayName, res.Session.Status, hostLabel, claimLabel, promptSize); err != nil {
				return err
			}
			if opts.claimPR != "" {
				return writeClaimPRCheckout(out, claim.BranchChanged)
			}
			return nil
		},
	}
	f := cmd.Flags()
	// --agent is an alias for --harness so the more intuitive `ao spawn --agent
	// droid` works identically; both resolve to the same harness flag.
	f.SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "agent" {
			name = "harness"
		}
		return pflag.NormalizedName(name)
	})
	f.StringVar(&opts.project, "project", "", "Project id to spawn the session in (default: AO_PROJECT_ID or the current registered repo)")
	f.BoolVar(&opts.standalone, "standalone", false, "Spawn a projectless worker in an AO-managed plain directory (requires --agent)")
	f.StringVar(&opts.harness, "harness", "", "Agent harness / --agent: claude-code, codex, aider, opencode, opencode-v2, grok, droid, amp, agy, crush, cursor, qwen, gemini, copilot, goose, auggie, continue, devin, cline, kimi, muse, kiro, kilocode, vibe, pi, kimchi, prime-agent, autohand, omp, fx, unreal-agent, mimo-code, deepseek-harness, openhands, command-code (default: project worker.agent; orchestrator spawns default to project orchestrator.agent; required if the project has none)")
	f.StringVar(&opts.kind, "kind", "", "Session role: worker, orchestrator, or coordinator (default: worker). A coordinator is spawned by the live root and needs --scope")
	f.StringVar(&opts.scope, "scope", "", "Bounded area of work for --kind coordinator (required, max 200 characters)")
	f.StringVar(&opts.workerAgent, "worker-agent", "", "Agent selection for the coordinator's workers (--kind coordinator): inherit, fixed:<profile-id> or random[:<profile-id>,...] over the project's agent profiles")
	f.StringVar(&opts.coordinatorAgent, "coordinator-agent", "", "Agent selection for the coordinator itself (--kind coordinator): inherit, fixed:<profile-id> or random[:<profile-id>,...]")
	f.StringVar(&opts.mode, "mode", "", "Initial session interface: chat (structured agent connection) or tui (the agent's native terminal). Omitted uses the daemon default; compatible sessions can switch later.")
	f.StringVar(&opts.branch, "branch", "", "Branch for git project sessions (default: ao/<session-id>/root; unsupported for standalone or Scratch sessions)")
	f.StringVar(&opts.prompt, "prompt", "", "Initial prompt for the agent")
	f.StringVar(&opts.model, "model", "", "Agent model override for this session only (e.g. sonnet, gpt-5.6-sol); overrides project/role config without changing it. The value must be valid for the resolved harness or the daemon blocks the spawn")
	f.StringVar(&opts.effort, "effort", "", "Reasoning effort for the selected model (e.g. low, medium, high); must be one the model advertises or the daemon blocks the spawn")
	f.StringVar(&opts.output, "output", "", "Deliverable for the session: artifact asks the agent to write a file in its artifact directory instead of opening a PR (default: ordinary task)")
	f.BoolVar(&opts.terminateOnPRMerge, "terminate-on-pr-merge", false, "Tear down this session after all its PRs reach a terminal state with at least one merged and the agent quiesces; overrides the project default")
	f.StringVar(&opts.issue, "issue", "", "Issue id to associate with the session")
	f.StringVar(&opts.trackerProvider, "tracker-provider", "github", "Issue tracker provider: github or gitlab (default: github)")
	f.StringVar(&opts.name, "name", "", "Display name shown in the sidebar (required, max 100 characters)")
	f.StringVar(&opts.claimPR, "claim-pr", "", "Claim PR ownership metadata only for the spawned session; does not check out the PR branch")
	f.BoolVar(&opts.noTakeover, "no-takeover", false, "Refuse if another active session owns the claimed PR (requires --claim-pr)")
	f.BoolVar(&opts.skipAgentCheck, "skip-agent-check", false, "Skip CLI readiness warnings (the daemon still validates launch readiness)")
	addHostFlag(cmd, ctx)
	return cmd
}

// linkRemoteWorker tells this machine's daemon that a worker it cannot see was
// spawned on a saved remote host, so the daemon pulls the worker's reports and
// delivers them to the parent's project orchestrator.
// It returns why the daemon will not poll that host, when it will not.
func (c *commandContext) linkRemoteWorker(ctx context.Context, parentID string, res spawnResult) (string, error) {
	req := remoteWorkerLinkRequest{
		ParentSessionID: parentID, HostID: c.remote.hostID,
		RemoteSessionID: res.Session.ID, RemoteProjectID: res.Session.ProjectID,
		DisplayName: res.Session.DisplayName,
	}
	var link struct {
		PullBlocked string `json:"pullBlocked"`
	}
	if err := c.localContext().postJSON(ctx, "remote-worker-links", req, &link); err != nil {
		return "", err
	}
	return link.PullBlocked, nil
}

// checkRemoteWorkerCapacity asks the local daemon if a link would pass its
// capacity gates; a daemon without the route is skipped.
func (c *commandContext) checkRemoteWorkerCapacity(ctx context.Context, parentID string) error {
	var ok struct{}
	err := c.localContext().postJSON(ctx, "remote-worker-links/check", struct {
		ParentSessionID string `json:"parentSessionId"`
	}{parentID}, &ok)
	var apiErr apiResponseError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound && apiErr.ErrorBody.Code != "SESSION_NOT_FOUND" {
		return nil
	}
	return err
}

// remoteWorkerAgent mirrors the daemon's RemoteWorkerAgent body.
type remoteWorkerAgent struct {
	ProfileID string `json:"profileId,omitempty"`
	Harness   string `json:"harness"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
}

// remoteAgentCheckRequest and remoteAgentCheckResponse mirror the agent half
// of POST /api/v1/remote-worker-links/check.
type remoteAgentCheckRequest struct {
	ParentSessionID      string            `json:"parentSessionId"`
	HostID               string            `json:"hostId"`
	Harness              string            `json:"harness,omitempty"`
	HarnessExplicit      bool              `json:"harnessExplicit,omitempty"`
	Model                string            `json:"model,omitempty"`
	Effort               string            `json:"effort,omitempty"`
	HostReadinessChecked bool              `json:"hostReadinessChecked,omitempty"`
	UnreadyHarnesses     map[string]string `json:"unreadyHarnesses,omitempty"`
}

type remoteAgentCheckResponse struct {
	ReadinessHarnesses []string           `json:"readinessHarnesses,omitempty"`
	Agent              *remoteWorkerAgent `json:"agent,omitempty"`
}

// checkRemoteWorkerAgent asks the local daemon for a remote worker's profile
// agent, reporting the host's readiness. Nil keeps normal resolution.
func (c *commandContext) checkRemoteWorkerAgent(ctx context.Context, parentID string, req *spawnRequest, harnessExplicit bool) (*remoteWorkerAgent, error) {
	check := remoteAgentCheckRequest{
		ParentSessionID: parentID, HostID: c.remote.hostID, Harness: req.Harness,
		HarnessExplicit: harnessExplicit, Model: req.Model, Effort: req.Effort,
	}
	var res remoteAgentCheckResponse
	for range 2 {
		res = remoteAgentCheckResponse{}
		err := c.localContext().postJSON(ctx, "remote-worker-links/check", check, &res)
		var apiErr apiResponseError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound && apiErr.ErrorBody.Code != "SESSION_NOT_FOUND" {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if len(res.ReadinessHarnesses) == 0 {
			return res.Agent, nil
		}
		readiness, err := c.ensureAgentReadiness(ctx, res.ReadinessHarnesses, "launch")
		if err != nil {
			return nil, fmt.Errorf("check agent readiness on host %q: %w", c.remote.label, err)
		}
		check.HostReadinessChecked, check.UnreadyHarnesses = true, unreadyHarnesses(res.ReadinessHarnesses, readiness)
	}
	return nil, fmt.Errorf("the daemon kept asking for host readiness while choosing an agent profile")
}

// unreadyHarnesses names the asked harnesses the host cannot use. An unknown
// state is not excluded, as for a local launch.
func unreadyHarnesses(asked []string, readiness agentReadinessResponse) map[string]string {
	known := map[string]agentReadinessSnapshot{}
	for _, snapshot := range readiness.Agents {
		known[snapshot.ID] = snapshot
	}
	out := map[string]string{}
	for _, id := range asked {
		snapshot, ok := known[id]
		switch {
		case !ok:
			out[id] = "not supported on the host"
		case snapshot.EffectiveReadiness == "not_ready" && snapshot.Installation.State == "not_installed":
			out[id] = "not_installed"
		case snapshot.EffectiveReadiness == "not_ready":
			out[id] = "unauthorized"
		}
	}
	return out
}

// failRemoteLink handles a link rejected after the remote spawn: capacity or
// ownership terminates the remote session, other failures keep it.
func (c *commandContext) failRemoteLink(ctx context.Context, remoteID string, err error) error {
	var apiErr apiResponseError
	rejected := errors.As(err, &apiErr) && (strings.Contains(apiErr.ErrorBody.Code, "LIMIT") || strings.HasPrefix(apiErr.ErrorBody.Code, "OWNER"))
	if !rejected {
		return fmt.Errorf("remote worker %s is still running but was not linked, so its reports will not reach this machine's orchestrator: %w", remoteID, err)
	}
	if killErr := c.rollbackSpawnedSession(ctx, remoteID); killErr != nil {
		return fmt.Errorf("remote worker %s was not linked: %w; terminating it failed, clean it up manually: %w", remoteID, err, killErr)
	}
	return fmt.Errorf("remote worker %s was not linked and was terminated: %w", remoteID, err)
}

func (c *commandContext) fetchAgentInventory(ctx context.Context, refresh bool) (agentInventory, error) {
	if refresh {
		var inventory agentInventory
		if err := c.postJSON(ctx, "agents/refresh", struct{}{}, &inventory); err != nil {
			return agentInventory{}, err
		}
		return inventory, nil
	}
	readiness, err := c.ensureAgentReadiness(ctx, nil, "display")
	if err != nil {
		return agentInventory{}, err
	}
	return readinessInventory(readiness), nil
}

func (c *commandContext) resolveSpawnProject(ctx context.Context, explicit string) (projectDetails, error) {
	if id := strings.TrimSpace(explicit); id != "" {
		return c.fetchProjectDetails(ctx, id)
	}
	if c.targetsRemote() {
		return c.resolveRemoteSpawnProject(ctx)
	}
	if id := strings.TrimSpace(os.Getenv("AO_PROJECT_ID")); id != "" {
		return c.fetchProjectDetails(ctx, id)
	}
	if sessionID := strings.TrimSpace(os.Getenv("AO_SESSION_ID")); sessionID != "" {
		project, err := c.resolveProjectFromSession(ctx, sessionID)
		if err != nil {
			return projectDetails{}, err
		}
		return project, nil
	}
	project, ok, err := c.resolveProjectFromCWD(ctx)
	if err != nil {
		return projectDetails{}, err
	}
	if ok {
		return project, nil
	}
	return projectDetails{}, usageError{fmt.Errorf("project could not be resolved; pass --project, use --standalone, or run `ao project add --path <repo-path> --worker-agent <agent>`")}
}

// resolveRemoteSpawnProject picks the remote project for `spawn --host` when
// --project is omitted. Local project IDs, session IDs, and paths mean nothing
// on another daemon, so it matches the local context project's repository
// against the remote host's projects and requires exactly one match.
func (c *commandContext) resolveRemoteSpawnProject(ctx context.Context) (projectDetails, error) {
	localKey := ""
	if local, err := c.localContext().resolveSpawnProject(ctx, ""); err == nil {
		localKey = projectRepoKey(local)
	}
	var list projectListResult
	if err := c.getJSON(ctx, "projects", &list); err != nil {
		return projectDetails{}, err
	}
	sort.Slice(list.Projects, func(i, j int) bool { return list.Projects[i].ID < list.Projects[j].ID })
	available := make([]string, 0, len(list.Projects))
	var matches []projectDetails
	for _, summary := range list.Projects {
		available = append(available, summary.ID)
		if localKey == "" {
			continue
		}
		project, err := c.fetchProjectDetails(ctx, summary.ID)
		if err != nil {
			return projectDetails{}, err
		}
		if projectRepoKey(project) == localKey {
			matches = append(matches, project)
		}
	}
	remoteProjects := "none"
	if len(available) > 0 {
		remoteProjects = strings.Join(available, ", ")
	}
	switch {
	case localKey == "":
		return projectDetails{}, usageError{fmt.Errorf("--project is required with --host when the local project's repository is unknown (remote projects: %s)", remoteProjects)}
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) == 0:
		return projectDetails{}, usageError{fmt.Errorf("no project on the remote host uses repository %s; pass --project (remote projects: %s)", localKey, remoteProjects)}
	default:
		ids := make([]string, 0, len(matches))
		for _, match := range matches {
			ids = append(ids, match.ID)
		}
		return projectDetails{}, usageError{fmt.Errorf("several remote projects use repository %s (%s); pass --project", localKey, strings.Join(ids, ", "))}
	}
}

// projectRepoKey normalizes a project's repository to host/owner/name so the
// same repository matches across machines regardless of SSH vs HTTPS clones.
func projectRepoKey(project projectDetails) string {
	repo := strings.TrimSpace(project.Repo)
	if project.Config != nil && strings.TrimSpace(project.Config.CanonicalRepoURL) != "" {
		repo = strings.TrimSpace(project.Config.CanonicalRepoURL)
	}
	host, owner, name, err := cliRepoFromURL(repo)
	if err != nil || host == "" || owner == "" || name == "" {
		return ""
	}
	return strings.ToLower(host + "/" + owner + "/" + strings.TrimSuffix(name, ".git"))
}

func (c *commandContext) resolveProjectFromSession(ctx context.Context, sessionID string) (projectDetails, error) {
	sess, err := c.fetchScopedSession(ctx, sessionID, "")
	if err != nil {
		return projectDetails{}, usageError{fmt.Errorf("project could not be resolved from AO_SESSION_ID %q; pass --project", sessionID)}
	}
	if strings.TrimSpace(sess.ProjectID) == "" {
		return projectDetails{}, usageError{fmt.Errorf("project could not be resolved from AO_SESSION_ID %q; pass --project", sessionID)}
	}
	return c.fetchProjectDetails(ctx, sess.ProjectID)
}

func (c *commandContext) resolveProjectFromCWD(ctx context.Context) (projectDetails, bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return projectDetails{}, false, err
	}
	cwd, err = normalizeProjectMatchPath(cwd)
	if err != nil {
		return projectDetails{}, false, err
	}

	var list projectListResult
	if err := c.getJSON(ctx, "projects", &list); err != nil {
		return projectDetails{}, false, err
	}
	sort.Slice(list.Projects, func(i, j int) bool {
		return list.Projects[i].ID < list.Projects[j].ID
	})

	var best projectDetails
	bestLen := -1
	ambiguous := false
	for _, summary := range list.Projects {
		project, err := c.fetchProjectDetails(ctx, summary.ID)
		if err != nil {
			return projectDetails{}, false, err
		}
		if project.Path == "" {
			continue
		}
		projectPath, err := normalizeProjectMatchPath(project.Path)
		if err != nil {
			continue
		}
		if !pathContains(projectPath, cwd) {
			continue
		}
		pathLen := len(projectPath)
		switch {
		case pathLen > bestLen:
			best = project
			bestLen = pathLen
			ambiguous = false
		case pathLen == bestLen:
			ambiguous = true
		}
	}
	if bestLen == -1 {
		return projectDetails{}, false, nil
	}
	if ambiguous {
		return projectDetails{}, false, usageError{fmt.Errorf("current directory matches multiple registered projects; pass --project")}
	}
	return best, true, nil
}

func isScratchProject(project projectDetails) bool {
	return project.ID == "scratch" && project.Kind == "scratch"
}

func normalizeProjectMatchPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if realPath, err := filepath.EvalSymlinks(abs); err == nil {
		abs = realPath
	}
	return filepath.Clean(abs), nil
}

func pathContains(root, child string) bool {
	if root == child {
		return true
	}
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// validateCoordinatorSpawn rejects misuse of --scope and --kind coordinator
// before any daemon call, so it exits 2 as a usage error.
func validateCoordinatorSpawn(opts spawnOptions, remote bool) error {
	scope := strings.TrimSpace(opts.scope)
	if opts.kind != "coordinator" {
		if scope != "" {
			return usageError{fmt.Errorf("--scope is supported with --kind coordinator only")}
		}
		if strings.TrimSpace(opts.workerAgent) != "" || strings.TrimSpace(opts.coordinatorAgent) != "" {
			return usageError{fmt.Errorf("--worker-agent and --coordinator-agent are supported with --kind coordinator only")}
		}
		return nil
	}
	if scope == "" {
		return usageError{fmt.Errorf("--scope is required with --kind coordinator")}
	}
	if utf8.RuneCountInString(scope) > maxCoordinatorScopeLen {
		return usageError{fmt.Errorf("--scope must be %d characters or fewer", maxCoordinatorScopeLen)}
	}
	if remote {
		return usageError{fmt.Errorf("--host is not supported with --kind coordinator; coordinators run where the root runs")}
	}
	if strings.TrimSpace(opts.claimPR) != "" {
		return usageError{fmt.Errorf("--claim-pr is supported for worker sessions only")}
	}
	return nil
}

// coordinatorAgentSelections parses the two agent selection flags; both are
// nil unless set.
func coordinatorAgentSelections(opts spawnOptions) (worker, coordinator *agentSelection, err error) {
	if v := strings.TrimSpace(opts.workerAgent); v != "" {
		if worker, err = parseAgentSelection("--worker-agent", v); err != nil {
			return nil, nil, err
		}
	}
	if v := strings.TrimSpace(opts.coordinatorAgent); v != "" {
		if coordinator, err = parseAgentSelection("--coordinator-agent", v); err != nil {
			return nil, nil, err
		}
	}
	return worker, coordinator, nil
}

func spawnBoolPtr(value bool) *bool { return &value }

func resolveSpawnHarness(explicit, kind string, project projectDetails) (string, error) {
	if harness := strings.TrimSpace(explicit); harness != "" {
		return harness, nil
	}
	if project.Config != nil {
		if kind == "orchestrator" || kind == "coordinator" {
			if harness := strings.TrimSpace(project.Config.Orchestrator.Agent); harness != "" {
				return harness, nil
			}
		} else {
			if harness := strings.TrimSpace(project.Config.Worker.Agent); harness != "" {
				return harness, nil
			}
		}
	}
	// The target daemon reports its AO_AGENT default in project.Agent. A role
	// that is not configured falls back to that machine default.
	if harness := strings.TrimSpace(project.Agent); harness != "" {
		return harness, nil
	}
	if kind == "orchestrator" || kind == "coordinator" {
		return "", usageError{fmt.Errorf("agent could not be resolved; pass --agent or configure `ao project set-config %s --orchestrator-agent <agent>`", project.ID)}
	}
	return "", usageError{fmt.Errorf("agent could not be resolved; pass --agent or configure `ao project set-config %s --worker-agent <agent>`", project.ID)}
}

func (c *commandContext) preflightSpawnAgentAuth(ctx context.Context, cmd *cobra.Command, agentID string) error {
	readiness, err := c.ensureAgentReadiness(ctx, []string{agentID}, "launch")
	if err != nil {
		var apiErr apiResponseError
		if errors.As(err, &apiErr) && apiErr.ErrorBody.Code == "UNKNOWN_AGENT_ID" {
			return fmt.Errorf("agent %q is not supported by this daemon; pass a supported --agent or run `ao agent ls`", agentID)
		}
		return err
	}
	if len(readiness.Agents) != 1 {
		return fmt.Errorf("agent %q is not supported by this daemon; pass a supported --agent or run `ao agent ls`", agentID)
	}
	snapshot := readiness.Agents[0]
	if snapshot.Installation.State == "not_installed" {
		return fmt.Errorf("agent %q needs install; install the agent CLI before spawning", agentID)
	}
	if snapshot.Installation.State == "unknown" {
		_, err = fmt.Fprintf(cmd.ErrOrStderr(), "warning: agent %q installation status is unknown; continuing and letting spawn validate runtime readiness\n", agentID)
		return err
	}
	if snapshot.Authentication.State == "authorized" || snapshot.Authentication.State == "not_applicable" {
		return nil
	}
	if snapshot.Authentication.State == "unauthorized" {
		_, err = fmt.Fprintf(cmd.ErrOrStderr(), "warning: agent %q may need auth according to daemon readiness; continuing and letting spawn validate runtime readiness\n", agentID)
		return err
	}
	_, err = fmt.Fprintf(cmd.ErrOrStderr(), "warning: agent %q auth status is unknown; continuing and letting spawn validate runtime readiness\n", agentID)
	return err
}

func (c *commandContext) ensureAgentReadiness(ctx context.Context, agentIDs []string, purpose string) (agentReadinessResponse, error) {
	var result agentReadinessResponse
	request := ensureAgentReadinessRequest{AgentIDs: agentIDs, Purpose: purpose}
	if err := c.postJSON(ctx, "agents/readiness/ensure", request, &result); err != nil {
		return agentReadinessResponse{}, err
	}
	return result, nil
}

// rollbackSpawnedSession reverses a partial `spawn` whose out-of-band follow-up
// (PR claim) failed. It calls the daemon's `/rollback` endpoint, which deletes
// the seed-state row outright instead of marking it terminated — so the user
// does not see an orphan terminated session under `--include-terminated`. If
// spawn output has already landed (workspace + runtime), the daemon falls back
// to a Kill on the server side so teardown still happens.
func (c *commandContext) rollbackSpawnedSession(ctx context.Context, id string) error {
	var res rollbackSessionResponse
	return c.postJSON(ctx, "sessions/"+url.PathEscape(id)+"/rollback", struct{}{}, &res)
}

// rollbackSessionResponse mirrors the daemon's RollbackSessionResponse body.
type rollbackSessionResponse struct {
	OK        bool   `json:"ok"`
	SessionID string `json:"sessionId"`
	Deleted   bool   `json:"deleted,omitempty"`
	Killed    bool   `json:"killed,omitempty"`
}

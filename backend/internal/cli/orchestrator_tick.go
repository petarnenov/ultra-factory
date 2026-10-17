package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	scmgithub "github.com/aoagents/agent-orchestrator/backend/internal/adapters/scm/github"
	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/orchestratortick"
)

// newOrchestratorTickCommand is the orchestrator wake-up loop (package
// orchestratortick). It is configured by the RW_TICK_* variables.
func newOrchestratorTickCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tick",
		Short: "Wake the idle orchestrator with worker, PR and CI state",
		Long: "Wakes the project's idle orchestrator with the state of its workers, pull requests and CI, " +
			"and with the invariants of its reconcile loop that do not hold. It sends only when the state " +
			"changed or a re-nudge is due, and never interrupts an active turn. Configuration comes from the " +
			"RW_TICK_* environment variables (see deploy/railway/README.md); GitHub is read with AO_GITHUB_TOKEN, " +
			"GH_TOKEN, GITHUB_TOKEN or `gh auth token`.",
	}
	var dryRun bool
	run := &cobra.Command{
		Use:   "run",
		Short: "Run one tick",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := ctx.tickRunner(dryRun)
			if err != nil {
				return err
			}
			res, err := r.Tick(cmd.Context())
			if err != nil {
				return err
			}
			if dryRun {
				return nil
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", res.Outcome, res.Reason)
			return err
		},
	}
	run.Flags().BoolVar(&dryRun, "dry-run", false, "Gather and decide, print the message, but never send or record a send")
	loop := &cobra.Command{
		Use:   "loop",
		Short: "Run a tick now and then every RW_TICK_INTERVAL seconds until SIGINT or SIGTERM",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := ctx.tickRunner(false)
			if err != nil {
				return err
			}
			sigCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return r.Loop(sigCtx)
		},
	}
	snapshot := &cobra.Command{
		Use:   "snapshot",
		Short: "Print the state a tick would report, without sending or recording anything",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := ctx.tickRunner(true)
			if err != nil {
				return err
			}
			facts := r.Gather(cmd.Context())
			violations := orchestratortick.Violations(r.Config, facts, ctx.deps.Now())
			_, err = io.WriteString(cmd.OutOrStdout(), orchestratortick.Snapshot(r.Config, facts, violations))
			return err
		},
	}
	pause := &cobra.Command{
		Use:   "pause [reason]",
		Short: "Set the maintenance flag: every tick skips until resume",
		Args:  atMostOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := tickStore()
			if err != nil {
				return err
			}
			reason := "maintenance"
			if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
				reason = strings.TrimSpace(args[0])
			}
			reason = fmt.Sprintf("%s (by %s at %s)", reason, currentUser(), ctx.deps.Now().UTC().Format(time.RFC3339))
			if err := store.Pause(reason); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "paused: %s\n", reason)
			return err
		},
	}
	resume := &cobra.Command{
		Use:   "resume",
		Short: "Clear the maintenance flag",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := tickStore()
			if err != nil {
				return err
			}
			was, err := store.Resume()
			if err != nil {
				return err
			}
			msg := "resumed"
			if !was {
				msg = "not paused"
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), msg)
			return err
		},
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show the maintenance flag, the last tick and the last message sent",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := tickStore()
			if err != nil {
				return err
			}
			return writeTickStatus(cmd.OutOrStdout(), store)
		},
	}
	cmd.AddCommand(run, loop, snapshot, pause, resume, status)
	return cmd
}

// tickStore keeps the tick's state under the AO state directory.
func tickStore() (orchestratortick.FileStore, error) {
	cfg, err := config.Load()
	if err != nil {
		return orchestratortick.FileStore{}, err
	}
	return orchestratortick.FileStore{Dir: filepath.Join(cfg.StateDir, "orchestrator-tick")}, nil
}

func (c *commandContext) tickRunner(dryRun bool) (*orchestratortick.Runner, error) {
	cfg, err := orchestratortick.ConfigFromEnv(os.Getenv)
	if err != nil {
		return nil, usageError{err}
	}
	store, err := tickStore()
	if err != nil {
		return nil, err
	}
	tokens := scmgithub.FallbackTokenSource{
		scmgithub.EnvTokenSource{EnvVars: []string{"AO_GITHUB_TOKEN", "GH_TOKEN"}},
		&scmgithub.GHTokenSource{},
	}
	return &orchestratortick.Runner{
		Config:    cfg,
		Daemon:    tickDaemon{c: c},
		GitHub:    tickGitHub{client: scmgithub.NewClient(scmgithub.ClientOptions{Token: tokens, UserAgent: "ao-orchestrator-tick"})},
		Store:     store,
		Log:       slog.New(slog.NewTextHandler(c.deps.Err, nil)),
		Now:       c.deps.Now,
		DryRun:    dryRun,
		DryRunOut: c.deps.Out,
	}, nil
}

func writeTickStatus(w io.Writer, store orchestratortick.Store) error {
	if reason, paused, err := store.Paused(); err != nil {
		return err
	} else if paused {
		if _, err := fmt.Fprintf(w, "paused: %s\n", reason); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(w, "not paused"); err != nil {
		return err
	}
	if rec, ok, err := store.LastTick(); err != nil {
		return err
	} else if ok {
		if _, err := fmt.Fprintf(w, "last tick: %s %s %s\n", rec.At.Format(time.RFC3339), rec.Outcome, rec.Reason); err != nil {
			return err
		}
		for _, v := range rec.Violations {
			if _, err := fmt.Fprintf(w, "  VIOLATION: %s\n", v); err != nil {
				return err
			}
		}
	} else if _, err := fmt.Fprintln(w, "last tick: none"); err != nil {
		return err
	}
	if sent, ok, err := store.LastSent(); err != nil {
		return err
	} else if ok {
		_, err = fmt.Fprintf(w, "last sent: %s to %s\n", sent.At.Format(time.RFC3339), sent.Session)
		return err
	}
	_, err := fmt.Fprintln(w, "last sent: none")
	return err
}

func currentUser() string {
	if u := strings.TrimSpace(os.Getenv("USER")); u != "" {
		return u
	}
	return fmt.Sprintf("uid %d", os.Getuid())
}

// tickDaemon reads the local daemon and, through saved remote hosts, the
// worker sessions on each host.
type tickDaemon struct{ c *commandContext }

func (d tickDaemon) Status(ctx context.Context) (orchestratortick.DaemonStatus, error) {
	st, err := d.c.inspectDaemon(ctx)
	if err != nil {
		return orchestratortick.DaemonStatus{}, err
	}
	return orchestratortick.DaemonStatus{
		Ready:  st.State == stateReady && st.Health == "ok",
		Detail: fmt.Sprintf("state=%s health=%s", st.State, st.Health),
	}, nil
}

func (d tickDaemon) Orchestrators(ctx context.Context) ([]orchestratortick.Orchestrator, error) {
	var res sessionListResponse
	if err := d.c.getJSON(ctx, "orchestrators", &res); err != nil {
		return nil, err
	}
	var out []orchestratortick.Orchestrator
	for _, s := range res.Sessions {
		if s.Kind == "orchestrator" && !s.IsTerminated {
			out = append(out, orchestratortick.Orchestrator{ID: s.ID, ProjectID: s.ProjectID})
		}
	}
	return out, nil
}

func (d tickDaemon) Session(ctx context.Context, id string) (orchestratortick.SessionState, error) {
	s, err := d.c.fetchScopedSession(ctx, id, "")
	if err != nil {
		return orchestratortick.SessionState{}, err
	}
	return orchestratortick.SessionState{Status: s.Status, Activity: s.Activity.State, Terminated: s.IsTerminated}, nil
}

func (d tickDaemon) Workers(ctx context.Context, host string) ([]orchestratortick.Worker, error) {
	// A remote target is cached per command context, so each host gets its own.
	hc := &commandContext{deps: d.c.deps, hostSelector: host}
	var res sessionListResponse
	if err := hc.getJSON(ctx, "sessions?active=true", &res); err != nil {
		return nil, err
	}
	sessions := filterAndSortSessions(res.Sessions, false)
	out := make([]orchestratortick.Worker, 0, len(sessions))
	for _, s := range sessions {
		w := orchestratortick.Worker{
			ID: s.ID, Branch: s.Branch, Activity: s.Activity.State, Harness: s.Harness,
			Terminated: s.IsTerminated, LastActivityAt: s.Activity.LastActivityAt, CreatedAt: s.CreatedAt,
		}
		if len(s.PRs) > 0 {
			w.PRNumber, w.CI = s.PRs[0].Number, s.PRs[0].CI
		}
		out = append(out, w)
	}
	return out, nil
}

func (d tickDaemon) Send(ctx context.Context, sessionID, message string) error {
	quiet := &commandContext{deps: d.c.deps}
	quiet.deps.Out = io.Discard
	return quiet.sendMessage(ctx, sendOptions{session: sessionID, message: message})
}

// tickGitHub reads the repository state in one GraphQL query.
type tickGitHub struct{ client *scmgithub.Client }

const tickGitHubQuery = `query($owner: String!, $name: String!, $base: String!, $baseRef: String!, $main: String!, $label: String!) {
  repository(owner: $owner, name: $name) {
    open: pullRequests(baseRefName: $base, states: OPEN, first: 50, orderBy: {field: CREATED_AT, direction: ASC}) {
      nodes {
        number
        headRefName
        mergeable
        commits(last: 1) { nodes { commit { statusCheckRollup { contexts(first: 100) { nodes {
          __typename
          ... on CheckRun { status conclusion completedAt }
          ... on StatusContext { state createdAt }
        } } } } } }
      }
    }
    merged: pullRequests(baseRefName: $base, states: MERGED, first: 50, orderBy: {field: UPDATED_AT, direction: DESC}) {
      nodes { number body }
    }
    issues(labels: [$label], states: OPEN, first: 100) { nodes { number title } }
    ref(qualifiedName: $baseRef) { compare(headRef: $main) { aheadBy commits(last: 5) { nodes { oid } } } }
  }
}`

type tickGitHubResponse struct {
	Repository *struct {
		Open struct {
			Nodes []struct {
				Number      int    `json:"number"`
				HeadRefName string `json:"headRefName"`
				Mergeable   string `json:"mergeable"`
				Commits     struct {
					Nodes []struct {
						Commit struct {
							StatusCheckRollup *struct {
								Contexts struct {
									Nodes []struct {
										Typename    string    `json:"__typename"`
										Status      string    `json:"status"`
										Conclusion  string    `json:"conclusion"`
										CompletedAt time.Time `json:"completedAt"`
										State       string    `json:"state"`
										CreatedAt   time.Time `json:"createdAt"`
									} `json:"nodes"`
								} `json:"contexts"`
							} `json:"statusCheckRollup"`
						} `json:"commit"`
					} `json:"nodes"`
				} `json:"commits"`
			} `json:"nodes"`
		} `json:"open"`
		Merged struct {
			Nodes []struct {
				Number int    `json:"number"`
				Body   string `json:"body"`
			} `json:"nodes"`
		} `json:"merged"`
		Issues struct {
			Nodes []struct {
				Number int    `json:"number"`
				Title  string `json:"title"`
			} `json:"nodes"`
		} `json:"issues"`
		Ref *struct {
			Compare struct {
				AheadBy int `json:"aheadBy"`
				Commits struct {
					Nodes []struct {
						OID string `json:"oid"`
					} `json:"nodes"`
				} `json:"commits"`
			} `json:"compare"`
		} `json:"ref"`
	} `json:"repository"`
}

func (g tickGitHub) State(ctx context.Context, q orchestratortick.GitHubQuery) (orchestratortick.GitHubState, error) {
	owner, name, _ := strings.Cut(q.Repo, "/")
	var resp tickGitHubResponse
	err := g.client.Query(ctx, tickGitHubQuery, map[string]any{
		"owner": owner, "name": name, "base": q.Base, "baseRef": "refs/heads/" + q.Base, "main": q.Main, "label": q.IssueLabel,
	}, &resp)
	if err != nil {
		return orchestratortick.GitHubState{}, err
	}
	return tickGitHubState(resp, q)
}

func tickGitHubState(resp tickGitHubResponse, q orchestratortick.GitHubQuery) (orchestratortick.GitHubState, error) {
	repo := resp.Repository
	if repo == nil {
		return orchestratortick.GitHubState{}, fmt.Errorf("repository %s not found", q.Repo)
	}
	if repo.Ref == nil {
		return orchestratortick.GitHubState{}, fmt.Errorf("branch %s not found in %s", q.Base, q.Repo)
	}
	state := orchestratortick.GitHubState{OpenIssues: map[int]string{}, BehindMain: repo.Ref.Compare.AheadBy}
	for _, c := range repo.Ref.Compare.Commits.Nodes {
		state.BehindSHAs = append(state.BehindSHAs, shortSHA(c.OID))
	}
	for _, pr := range repo.Open.Nodes {
		out := orchestratortick.PullRequest{Number: pr.Number, Head: pr.HeadRefName, Mergeable: pr.Mergeable}
		for _, commit := range pr.Commits.Nodes {
			rollup := commit.Commit.StatusCheckRollup
			if rollup == nil {
				continue
			}
			for _, c := range rollup.Contexts.Nodes {
				switch c.Typename {
				case "CheckRun":
					result := c.Conclusion
					if result == "" {
						result = c.Status
					}
					out.Checks = append(out.Checks, orchestratortick.Check{Result: result, CompletedAt: c.CompletedAt})
				case "StatusContext":
					out.Checks = append(out.Checks, orchestratortick.Check{Result: c.State, CompletedAt: c.CreatedAt})
				}
			}
		}
		state.OpenPRs = append(state.OpenPRs, out)
	}
	for _, pr := range repo.Merged.Nodes {
		state.MergedPRs = append(state.MergedPRs, orchestratortick.MergedPR{Number: pr.Number, Body: pr.Body})
	}
	for _, issue := range repo.Issues.Nodes {
		state.OpenIssues[issue.Number] = issue.Title
	}
	return state, nil
}

func shortSHA(oid string) string {
	if len(oid) > 7 {
		return oid[:7]
	}
	return oid
}

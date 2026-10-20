package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type mergePRResponse struct {
	OK       bool   `json:"ok"`
	PRNumber int    `json:"prNumber"`
	Method   string `json:"method"`
}

// mergePRRequest is the merge body: PR URL and head to merge, plus the
// optional requester the daemon checks for merge authority (ADR 0010).
type mergePRRequest struct {
	PRURL           string `json:"prUrl"`
	ExpectedHeadSHA string `json:"expectedHeadSha"`

	RequestedBySessionID string `json:"requestedBySessionId,omitempty"`
}

type resolveCommentsRequest struct {
	CommentIDs []string `json:"commentIds,omitempty"`
}

type resolveCommentsResponse struct {
	OK       bool `json:"ok"`
	Resolved int  `json:"resolved"`
}

func newPRCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Manage pull requests",
	}
	cmd.AddCommand(newPRMergeCommand(ctx))
	cmd.AddCommand(newPRResolveCommentsCommand(ctx))
	cmd.AddCommand(newPRReadyCommand(ctx))
	return cmd
}

type setPRReadyRequest struct {
	Ready                bool   `json:"ready"`
	RequestedBySessionID string `json:"requestedBySessionId,omitempty"`
}

type setPRReadyResponse struct {
	OK       bool   `json:"ok"`
	PRURL    string `json:"prUrl"`
	PRNumber int    `json:"prNumber"`
	HeadSHA  string `json:"headSha"`
	Ready    bool   `json:"ready"`
	Changed  bool   `json:"changed"`
}

func newPRReadyCommand(ctx *commandContext) *cobra.Command {
	var undo bool
	cmd := &cobra.Command{
		Use:   "ready [<session>]",
		Short: "Mark a session's pull request ready for review (or back to draft)",
		Long: "Mark the session's open pull request ready for review, or convert it back to a draft with\n" +
			"--undo. This is the manual override of the CI gate. Only the project root and the session's\n" +
			"owning coordinator may run it; a worker is refused with CI_GATE_FORBIDDEN. A pull request that\n" +
			"already has the requested state is left alone. With no argument it targets the current session.",
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			self := strings.TrimSpace(os.Getenv("AO_SESSION_ID"))
			target := self
			if len(args) == 1 {
				target = strings.TrimSpace(args[0])
			}
			caller := ""
			if !ctx.targetsRemote() {
				caller = self
			}
			if target == "" {
				return usageError{errors.New("session id is required outside an AO session")}
			}
			var res setPRReadyResponse
			if err := ctx.postJSON(cmd.Context(), "sessions/"+url.PathEscape(target)+"/pr/ready",
				setPRReadyRequest{Ready: !undo, RequestedBySessionID: caller}, &res); err != nil {
				return err
			}
			state := "ready for review"
			if !res.Ready {
				state = "a draft"
			}
			if !res.Changed {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "PR #%d is already %s\n", res.PRNumber, state)
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "PR #%d is now %s\n", res.PRNumber, state)
			return err
		},
	}
	cmd.Flags().BoolVar(&undo, "undo", false, "Convert the pull request back to a draft")
	addHostFlag(cmd, ctx)
	return cmd
}

func newPRMergeCommand(ctx *commandContext) *cobra.Command {
	var project, head string
	cmd := &cobra.Command{
		Use:   "merge <pr-number>",
		Short: "Merge a pull request",
		Long: "Merge a pull request that an AO session tracks. The CLI looks up the pull request's URL and\n" +
			"current head commit from the session that owns it and asks the daemon to merge exactly that\n" +
			"head; the daemon refuses if the head moved or the pull request is not ready. Pass --head to\n" +
			"pin the commit you reviewed instead of the one AO currently tracks.",
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			prNumber, err := normalizePRNumber(args[0])
			if err != nil {
				return err
			}
			head = strings.ToLower(strings.TrimSpace(head))
			if head != "" && !gitSHAFlagPattern.MatchString(head) {
				return usageError{errors.New("--head must be a full 40-character commit SHA")}
			}
			req, err := ctx.resolveMergeRequest(cmd.Context(), prNumber, strings.TrimSpace(project), head)
			if err != nil {
				return err
			}
			if !ctx.targetsRemote() {
				req.RequestedBySessionID = strings.TrimSpace(os.Getenv("AO_SESSION_ID"))
			}
			var res mergePRResponse
			if err := ctx.postJSON(cmd.Context(), "prs/"+url.PathEscape(prNumber)+"/merge", req, &res); err != nil {
				return err
			}
			if method := strings.TrimSpace(res.Method); method != "" {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "merged PR #%d using %s\n", res.PRNumber, method)
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "merged PR #%d\n", res.PRNumber)
			return err
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project id that owns the pull request (needed when PR numbers collide across projects)")
	cmd.Flags().StringVar(&head, "head", "", "Full head commit SHA you reviewed (default: the head AO currently tracks)")
	return cmd
}

var gitSHAFlagPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// resolveMergeRequest builds the merge body from the owning session's PR
// summary: its URL and current head commit.
func (c *commandContext) resolveMergeRequest(ctx context.Context, prNumber, project, head string) (mergePRRequest, error) {
	number, _ := strconv.Atoi(prNumber)
	params := url.Values{}
	if project != "" {
		params.Set("project", project)
	}
	var list sessionListResponse
	if err := c.getJSON(ctx, apiPath("sessions", params), &list); err != nil {
		return mergePRRequest{}, err
	}
	owners := map[string]string{} // PR URL -> a session id that tracks it
	for _, sess := range list.Sessions {
		for _, pr := range sess.PRs {
			if pr.Number != number || strings.TrimSpace(pr.URL) == "" {
				continue
			}
			if _, ok := owners[pr.URL]; !ok {
				owners[pr.URL] = sess.ID
			}
		}
	}
	if len(owners) == 0 {
		return mergePRRequest{}, fmt.Errorf("no AO session tracks PR #%d; claim it first with `ao session claim-pr`", number)
	}
	if len(owners) > 1 {
		urls := make([]string, 0, len(owners))
		for u := range owners {
			urls = append(urls, u)
		}
		sort.Strings(urls)
		return mergePRRequest{}, usageError{fmt.Errorf("PR #%d is tracked in several repositories (%s); pass --project to pick one", number, strings.Join(urls, ", "))}
	}
	var prURL, sessionID string
	for u, id := range owners {
		prURL, sessionID = u, id
	}
	if head != "" {
		return mergePRRequest{PRURL: prURL, ExpectedHeadSHA: head}, nil
	}
	var res sessionPRSummaryResponse
	if err := c.getJSON(ctx, "sessions/"+url.PathEscape(sessionID)+"/pr", &res); err != nil {
		return mergePRRequest{}, err
	}
	for _, pr := range res.PRs {
		tracked := strings.ToLower(strings.TrimSpace(pr.HeadSHA))
		if pr.URL == prURL && gitSHAFlagPattern.MatchString(tracked) {
			return mergePRRequest{PRURL: prURL, ExpectedHeadSHA: tracked}, nil
		}
	}
	return mergePRRequest{}, fmt.Errorf("PR #%d has no known head commit yet; wait for AO to observe it or pass --head", number)
}

func newPRResolveCommentsCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "resolve-comments <pr-number> [comment-id...]",
		Short: "Resolve review threads on a pull request (by comment or thread id)",
		Args:  usageArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			prNumber, err := normalizePRNumber(args[0])
			if err != nil {
				return err
			}
			commentIDs := make([]string, 0, len(args)-1)
			for _, id := range args[1:] {
				id = strings.TrimSpace(id)
				if id == "" {
					return usageError{errors.New("comment id must not be blank")}
				}
				commentIDs = append(commentIDs, id)
			}
			var res resolveCommentsResponse
			if err := ctx.postJSON(
				cmd.Context(),
				"prs/"+url.PathEscape(prNumber)+"/resolve-comments",
				resolveCommentsRequest{CommentIDs: commentIDs},
				&res,
			); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "resolved %d review thread(s) on PR #%s\n", res.Resolved, prNumber)
			return err
		},
	}
}

func normalizePRNumber(raw string) (string, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "#")
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return "", usageError{errors.New("PR number must be a positive integer")}
	}
	return strconv.Itoa(n), nil
}

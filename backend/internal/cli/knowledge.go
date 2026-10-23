package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

type knowledgeLessonRequest struct {
	SessionID string   `json:"sessionId"`
	Kind      string   `json:"kind,omitempty"`
	Text      string   `json:"text"`
	Topics    []string `json:"topics,omitempty"`
}

type knowledgeLessonResponse struct {
	ID     string `json:"id"`
	Merged bool   `json:"merged"`
}

type knowledgeItem struct {
	Section string   `json:"section"`
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Outcome string   `json:"outcome"`
	Text    string   `json:"text"`
	Lessons []string `json:"lessons"`
	Score   float64  `json:"score"`
}

type knowledgeSearchResponse struct {
	Items []knowledgeItem `json:"items"`
}

func newKnowledgeCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{Use: "knowledge", Short: "Record and search lessons in the shared knowledge graph"}
	cmd.AddCommand(newKnowledgeAddCommand(ctx), newKnowledgeSearchCommand(ctx))
	return cmd
}

func newKnowledgeAddCommand(ctx *commandContext) *cobra.Command {
	var kind string
	var topics []string
	cmd := &cobra.Command{
		Use:   "add <lesson text>",
		Short: "Record a reusable lesson from this worker's task",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.knowledgeAdd(cmd.Context(), strings.Join(args, " "), kind, topics)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "tip", "Lesson kind: pitfall, fix, tip, or context")
	cmd.Flags().StringArrayVar(&topics, "topic", nil, "Topic the lesson is about (repeatable, at most 5)")
	return cmd
}

func (c *commandContext) knowledgeAdd(ctx context.Context, text, kind string, topics []string) error {
	text = strings.TrimSpace(text)
	switch kind {
	case "pitfall", "fix", "tip", "context":
	default:
		return usageError{errors.New("usage: --kind must be pitfall, fix, tip, or context")}
	}
	if text == "" {
		return usageError{errors.New("usage: lesson text is required")}
	}
	if utf8.RuneCountInString(text) > 1000 {
		return usageError{errors.New("usage: lesson text must be at most 1000 characters")}
	}
	if len(topics) > 5 {
		return usageError{errors.New("usage: at most 5 --topic values")}
	}
	sessionID := strings.TrimSpace(os.Getenv("AO_SESSION_ID"))
	if sessionID == "" {
		return usageError{errors.New("usage: AO_SESSION_ID is required")}
	}
	var resp knowledgeLessonResponse
	req := knowledgeLessonRequest{SessionID: sessionID, Kind: kind, Text: text, Topics: topics}
	if err := c.postJSON(ctx, "knowledge/lessons", req, &resp); err != nil {
		return err
	}
	format := "Recorded lesson %s\n"
	if resp.Merged {
		format = "Confirmed existing lesson %s\n"
	}
	_, err := fmt.Fprintf(c.deps.Out, format, resp.ID)
	return err
}

func newKnowledgeSearchCommand(ctx *commandContext) *cobra.Command {
	var project string
	var limit int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "search <words>",
		Short: "Search lessons and past tasks of this project",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.knowledgeSearch(cmd.Context(), strings.Join(args, " "), project, limit, asJSON)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project id (defaults to the worker's project via AO_SESSION_ID)")
	cmd.Flags().IntVar(&limit, "limit", 5, "Results per section, 1 to 50")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print JSON")
	return cmd
}

func (c *commandContext) knowledgeSearch(ctx context.Context, text, project string, limit int, asJSON bool) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return usageError{errors.New("usage: search words are required")}
	}
	if limit < 1 || limit > 50 {
		return usageError{errors.New("usage: --limit must be between 1 and 50")}
	}
	q := url.Values{"q": {text}, "limit": {strconv.Itoa(limit)}}
	if project != "" {
		q.Set("projectId", project)
	} else if sid := strings.TrimSpace(os.Getenv("AO_SESSION_ID")); sid != "" {
		q.Set("sessionId", sid)
	} else {
		return usageError{errors.New("usage: --project is required outside a worker session")}
	}
	var resp knowledgeSearchResponse
	if err := c.getJSON(ctx, "knowledge/search?"+q.Encode(), &resp); err != nil {
		return err
	}
	if asJSON {
		return writeJSON(c.deps.Out, resp)
	}
	if len(resp.Items) == 0 {
		_, err := fmt.Fprintln(c.deps.Out, "No matching knowledge.")
		return err
	}
	for _, it := range resp.Items {
		label := it.Outcome
		if it.Section == "lesson" {
			label = it.Kind
		}
		line := fmt.Sprintf("%-8s [%s] %s", it.Section, label, it.Text)
		if it.Section != "lesson" {
			line = fmt.Sprintf("%-8s [%s] %s — %s", it.Section, label, it.Title, it.Text)
		}
		if _, err := fmt.Fprintln(c.deps.Out, strings.TrimRight(line, " —")); err != nil {
			return err
		}
		for _, l := range it.Lessons {
			if _, err := fmt.Fprintf(c.deps.Out, "         lesson: %s\n", l); err != nil {
				return err
			}
		}
	}
	return nil
}

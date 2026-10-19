package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

type sessionInputOptions struct {
	json      bool
	requestID string
	action    string
	answers   []string
}

// conversationInputSnapshotDTO is the part of the conversation snapshot this
// command reads. Pending structured inputs are timeline activities.
type conversationInputSnapshotDTO struct {
	SessionID  string                         `json:"sessionId"`
	Activities []conversationInputActivityDTO `json:"activities"`
}

type conversationInputActivityDTO struct {
	ActivityKind string         `json:"activityKind"`
	Status       string         `json:"status"`
	Summary      string         `json:"summary"`
	RequestID    string         `json:"requestId"`
	Detail       map[string]any `json:"detail"`
}

type pendingSessionInput struct {
	RequestID string         `json:"requestId"`
	Mode      string         `json:"mode"`
	Message   string         `json:"message"`
	URL       string         `json:"url,omitempty"`
	Schema    map[string]any `json:"schema,omitempty"`
}

type sessionInputList struct {
	SessionID string                `json:"sessionId"`
	Inputs    []pendingSessionInput `json:"inputs"`
}

// resolveSessionInputRequest mirrors the daemon's resolve DTO without
// importing the HTTP controller package into the thin CLI.
type resolveSessionInputRequest struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

type sessionInputResolved struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
	Action    string `json:"action"`
	Status    string `json:"status"`
}

func newSessionInputCommand(ctx *commandContext) *cobra.Command {
	var opts sessionInputOptions
	cmd := &cobra.Command{
		Use:   "input <id>",
		Short: "List or answer a session's pending structured question",
		Long: "List the structured questions a chat session is waiting on, or resolve one. " +
			"Without --action the command lists each pending request id, question, and fields. " +
			"With --action it answers that request; an accepted form takes one --answer per field.",
		Args: oneSessionIDArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := normalizeSessionID(args[0])
			if err != nil {
				return err
			}
			return ctx.sessionInput(cmd.Context(), cmd, id, opts)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&opts.json, "json", false, "Output as JSON")
	f.StringVar(&opts.requestID, "request-id", "", "Pending input request id to resolve")
	f.StringVar(&opts.action, "action", "", "Resolve the input: accept, decline, or cancel")
	f.StringArrayVar(&opts.answers, "answer", nil, "Structured answer as key=value (repeatable; accept only)")
	addHostFlag(cmd, ctx)
	return cmd
}

func (c *commandContext) sessionInput(
	ctx context.Context,
	cmd *cobra.Command,
	session string,
	opts sessionInputOptions,
) error {
	action := strings.TrimSpace(opts.action)
	requestID := strings.TrimSpace(opts.requestID)
	if action == "" {
		if requestID != "" {
			return usageError{errors.New("usage: --request-id requires --action")}
		}
		if len(opts.answers) > 0 {
			return usageError{errors.New("usage: --answer requires --action accept")}
		}
		list, err := c.listPendingSessionInputs(ctx, session)
		if err != nil {
			return err
		}
		if opts.json {
			return writeJSON(cmd.OutOrStdout(), list)
		}
		return c.writePendingSessionInputs(cmd, list)
	}

	if action != "accept" && action != "decline" && action != "cancel" {
		return usageError{fmt.Errorf("usage: --action must be accept, decline, or cancel, got %q", action)}
	}
	if requestID == "" {
		return usageError{errors.New("usage: --request-id is required with --action")}
	}
	var content map[string]any
	if len(opts.answers) > 0 {
		if action != "accept" {
			return usageError{errors.New("usage: --answer requires --action accept")}
		}
		var err error
		content, err = parseSessionInputAnswers(opts.answers)
		if err != nil {
			return err
		}
	}

	path := "sessions/" + url.PathEscape(session) +
		"/conversation/inputs/" + url.PathEscape(requestID) + "/resolve"
	request := resolveSessionInputRequest{Action: action, Content: content}
	if err := c.postJSON(ctx, path, request, nil); err != nil {
		return err
	}
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), sessionInputResolved{
			SessionID: session,
			RequestID: requestID,
			Action:    action,
			Status:    "resolved",
		})
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(),
		"Resolved pending input %s on session %s (%s).\n", requestID, session, action)
	return err
}

func (c *commandContext) listPendingSessionInputs(
	ctx context.Context,
	session string,
) (sessionInputList, error) {
	var snapshot conversationInputSnapshotDTO
	if err := c.getJSON(ctx, "sessions/"+url.PathEscape(session)+"/conversation", &snapshot); err != nil {
		return sessionInputList{}, err
	}
	out := sessionInputList{
		SessionID: snapshot.SessionID,
		Inputs:    make([]pendingSessionInput, 0),
	}
	if out.SessionID == "" {
		out.SessionID = session
	}
	for _, activity := range snapshot.Activities {
		if activity.ActivityKind != "user_input" || activity.Status != "pending" ||
			strings.TrimSpace(activity.RequestID) == "" {
			continue
		}
		item := pendingSessionInput{
			RequestID: activity.RequestID,
			Mode:      strings.TrimSpace(inputDetailString(activity.Detail, "inputMode")),
			Message:   strings.TrimSpace(inputDetailString(activity.Detail, "message")),
			URL:       inputDetailString(activity.Detail, "url"),
		}
		if item.Message == "" {
			item.Message = activity.Summary
		}
		if item.Mode == "" {
			item.Mode = "form"
		}
		if schema, ok := activity.Detail["schema"].(map[string]any); ok {
			item.Schema = schema
		}
		out.Inputs = append(out.Inputs, item)
	}
	return out, nil
}

func inputDetailString(detail map[string]any, key string) string {
	value, _ := detail[key].(string)
	return value
}

func (c *commandContext) writePendingSessionInputs(cmd *cobra.Command, list sessionInputList) error {
	out := cmd.OutOrStdout()
	if len(list.Inputs) == 0 {
		_, err := fmt.Fprintf(out, "session %s has no pending structured inputs\n", list.SessionID)
		return err
	}
	noun := pluralS(len(list.Inputs))
	if _, err := fmt.Fprintf(out, "session %s has %d pending structured input%s\n\n",
		list.SessionID, len(list.Inputs), noun); err != nil {
		return err
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "REQUEST ID\tMODE\tQUESTION"); err != nil {
		return err
	}
	for _, input := range list.Inputs {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\n",
			input.RequestID, input.Mode, strings.Join(strings.Fields(input.Message), " ")); err != nil {
			return err
		}
	}
	if err := table.Flush(); err != nil {
		return err
	}
	for _, input := range list.Inputs {
		if _, err := fmt.Fprintf(out, "\n%s\n", input.RequestID); err != nil {
			return err
		}
		if input.URL != "" {
			if _, err := fmt.Fprintf(out, "  URL: %s\n", input.URL); err != nil {
				return err
			}
		}
		for _, field := range sessionInputFieldLines(input.Schema) {
			if _, err := fmt.Fprintf(out, "  %s\n", field); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "  %s\n", c.sessionInputAnswerHint(list.SessionID, input)); err != nil {
			return err
		}
	}
	return nil
}

func (c *commandContext) sessionInputAnswerHint(session string, input pendingSessionInput) string {
	host := ""
	if c.targetsRemote() {
		host = " --host " + shellQuote(strings.TrimSpace(c.hostSelector))
	}
	hint := "Answer with: ao session input " + shellQuote(session) + host +
		" --request-id " + shellQuote(input.RequestID) + " --action accept"
	if input.Mode == "url" {
		return hint + "  (records consent; it does not open the URL)"
	}
	return hint + " --answer <field>=<value>"
}

func sessionInputFieldLines(schema map[string]any) []string {
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 {
		return nil
	}
	required := make(map[string]bool, len(properties))
	switch values := schema["required"].(type) {
	case []any:
		for _, value := range values {
			if name, ok := value.(string); ok {
				required[name] = true
			}
		}
	}
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		property, _ := properties[name].(map[string]any)
		line := name
		if title, _ := property["title"].(string); strings.TrimSpace(title) != "" {
			line += " (" + title + ")"
		}
		typeName, _ := property["type"].(string)
		if typeName == "" {
			typeName = "string"
		}
		line += " [" + typeName
		if required[name] {
			line += ", required"
		} else {
			line += ", optional"
		}
		line += "]"
		if options := sessionInputFieldOptions(property); len(options) > 0 {
			quoted := make([]string, 0, len(options))
			for _, option := range options {
				quoted = append(quoted, strconv.Quote(option))
			}
			line += " values: " + strings.Join(quoted, ", ")
		}
		lines = append(lines, line)
	}
	return lines
}

func sessionInputFieldOptions(property map[string]any) []string {
	source := property["oneOf"]
	if candidates, ok := source.([]any); !ok || len(candidates) == 0 {
		if items, ok := property["items"].(map[string]any); ok {
			source = items["anyOf"]
		}
	}
	if candidates, ok := source.([]any); ok && len(candidates) > 0 {
		options := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			entry, ok := candidate.(map[string]any)
			if !ok {
				continue
			}
			if value, ok := entry["const"].(string); ok {
				options = append(options, value)
			}
		}
		if len(options) > 0 {
			return options
		}
	}
	if candidates, ok := property["enum"].([]any); ok {
		options := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			if value, ok := candidate.(string); ok {
				options = append(options, value)
			}
		}
		return options
	}
	return nil
}

// parseSessionInputAnswers turns key=value flags into structured content.
// Valid JSON keeps its type; other values are strings for typed schemas.
func parseSessionInputAnswers(values []string) (map[string]any, error) {
	content := make(map[string]any, len(values))
	for _, raw := range values {
		key, value, found := strings.Cut(raw, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, usageError{fmt.Errorf("usage: --answer must be key=value, got %q", raw)}
		}
		if _, exists := content[key]; exists {
			return nil, usageError{fmt.Errorf("usage: --answer repeats field %q", key)}
		}
		var typed any
		if err := json.Unmarshal([]byte(value), &typed); err == nil {
			content[key] = typed
		} else {
			content[key] = value
		}
	}
	return content, nil
}

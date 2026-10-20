package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type sendOptions struct {
	session         string
	message         string
	steer           bool
	clientMessageID string
	recoverOnly     bool
}

// sendAPIRequest mirrors the daemon's SendSessionMessageRequest body for
// POST /api/v1/sessions/{id}/send. The CLI keeps its own copy so it need not
// import httpd.
type sendAPIRequest struct {
	UserAuthored    bool   `json:"userAuthored,omitempty"`
	SenderSessionID string `json:"senderSessionId,omitempty"`
	Message         string `json:"message"`
}

// The steering DTOs mirror the daemon conversation API without coupling the
// thin CLI to the HTTP controller package.
type conversationMessageAPIRequest struct {
	Text            string `json:"text"`
	ClientMessageID string `json:"clientMessageId"`
	SenderSessionID string `json:"senderSessionId,omitempty"`
	RecoverOnly     bool   `json:"recoverOnly,omitempty"`
}

type steerOrSendAPIResponse struct {
	Outcome        string `json:"outcome"`
	TurnID         string `json:"turnId"`
	ProviderTurnID string `json:"providerTurnId"`
	ActivityID     string `json:"activityId"`
	State          string `json:"state"`
	Duplicate      bool   `json:"duplicate"`
}

func newSendCommand(ctx *commandContext) *cobra.Command {
	var opts sendOptions
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a message to a running agent session",
		Long: "Deliver a message to a session's agent. A message that arrives while the " +
			"agent is mid-turn is queued and read when that turn ends. When the session " +
			"is waiting on a structured question, answer it first with `ao session input`: " +
			"the message stays queued until that question is answered.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.sendMessage(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.session, "session", "", "Session id (required)")
	cmd.Flags().StringVar(&opts.message, "message", "", "Message body (required unless --recover-only)")
	cmd.Flags().BoolVar(&opts.steer, "steer", false, "Steer an active Chat turn, or start the next turn when idle")
	cmd.Flags().StringVar(&opts.clientMessageID, "client-message-id", "", "Stable steering delivery handle")
	cmd.Flags().BoolVar(&opts.recoverOnly, "recover-only", false, "Recover a steering result without contacting the provider")
	addHostFlag(cmd, ctx)
	return cmd
}

func (c *commandContext) sendMessage(ctx context.Context, opts sendOptions) error {
	if opts.recoverOnly && !opts.steer {
		return usageError{errors.New("usage: --recover-only requires --steer")}
	}
	if strings.TrimSpace(opts.clientMessageID) != "" && !opts.steer {
		return usageError{errors.New("usage: --client-message-id requires --steer")}
	}
	if opts.recoverOnly && strings.TrimSpace(opts.clientMessageID) == "" {
		return usageError{errors.New("usage: --recover-only requires --client-message-id")}
	}
	if !opts.recoverOnly && strings.TrimSpace(opts.message) == "" {
		return usageError{errors.New("usage: --message is required")}
	}
	message := opts.message
	senderSessionID := strings.TrimSpace(os.Getenv("AO_SESSION_ID"))
	if sender := senderSessionID; !opts.recoverOnly && sender != "" {
		if c.targetsRemote() {
			sender = remoteSenderLabel(sender)
		}
		message = "[from " + sender + "] " + message
	}
	userAuthored := senderSessionID == ""
	if c.targetsRemote() {
		// senderSessionId is loopback attribution; the same ID names an unrelated
		// session on another daemon, so a remote send keeps only the text prefix.
		senderSessionID = ""
	}
	session := strings.TrimSpace(opts.session)
	if session == "" {
		return usageError{errors.New("usage: --session is required")}
	}

	// PathEscape: session ids are already "-"/digit safe, but may later come
	// from sanitized issue refs; keep the URL well-formed regardless.
	path := "sessions/" + url.PathEscape(session)
	if !opts.steer {
		return c.plainSend(ctx, path, session, message, senderSessionID, userAuthored)
	}
	return c.steerMessage(ctx, path, message, senderSessionID, strings.TrimSpace(opts.clientMessageID), opts.recoverOnly)
}

// sendTarget is what the daemon showed about the session just before the
// message was accepted.
type sendTarget struct {
	pendingInputs []pendingSessionInput
	busy          bool
}

// plainSend sends the message, then explains what the target can read now.
// A failed state read never changes whether the send itself succeeded.
func (c *commandContext) plainSend(
	ctx context.Context,
	path, session, message, senderSessionID string,
	userAuthored bool,
) error {
	target := c.sendTargetState(ctx, session)
	if err := c.postJSON(ctx, path+"/send", sendAPIRequest{
		Message: message, SenderSessionID: senderSessionID, UserAuthored: userAuthored,
	}, nil); err != nil {
		return err
	}
	return c.writeSendTarget(session, target)
}

// sendTargetState reads the two facts that decide whether a message can reach
// the agent now: unanswered structured inputs, and a turn already running.
func (c *commandContext) sendTargetState(ctx context.Context, session string) sendTarget {
	var snapshot conversationInputSnapshotDTO
	if err := c.getJSON(ctx, "sessions/"+url.PathEscape(session)+"/conversation", &snapshot); err == nil {
		return sendTarget{
			pendingInputs: pendingInputsFromActivities(snapshot.Activities),
			busy:          snapshot.Controller == "busy",
		}
	}
	// A terminal-mode session has no conversation; its activity carries the
	// same answer.
	var res sessionResponse
	if err := c.getJSON(ctx, "sessions/"+url.PathEscape(session), &res); err != nil {
		return sendTarget{}
	}
	return sendTarget{busy: res.Session.Activity.State == string(domain.ActivityActive)}
}

// writeSendTarget reports what the caller's message is waiting behind. A send
// the agent can read immediately stays silent, as it always has.
func (c *commandContext) writeSendTarget(session string, target sendTarget) error {
	out := c.deps.Out
	if len(target.pendingInputs) > 0 {
		count := len(target.pendingInputs)
		unblocker := "the question is"
		if count > 1 {
			unblocker = "the questions are"
		}
		if _, err := fmt.Fprintf(out,
			"Message accepted and queued behind %d unanswered structured input%s on session %s.\n"+
				"The agent will not read it until %s answered.\n",
			count, pluralS(count), session, unblocker); err != nil {
			return err
		}
		for _, input := range target.pendingInputs {
			if _, err := fmt.Fprintf(out, "  %s\n", c.sessionInputAnswerHint(session, input)); err != nil {
				return err
			}
		}
		return nil
	}
	if target.busy {
		_, err := fmt.Fprintf(out,
			"Message accepted and queued behind the turn already running on session %s;"+
				" the agent reads it when that turn ends.\n", session)
		return err
	}
	return nil
}

func (c *commandContext) steerMessage(
	ctx context.Context,
	sessionPath, message, senderSessionID, clientMessageID string,
	recoverOnly bool,
) error {
	if clientMessageID == "" {
		clientMessageID = uuid.NewString()
	}
	var result steerOrSendAPIResponse
	err := c.postJSON(ctx, sessionPath+"/conversation/steer-or-send", conversationMessageAPIRequest{
		Text: message, ClientMessageID: clientMessageID, SenderSessionID: senderSessionID, RecoverOnly: recoverOnly,
	}, &result)
	if err != nil {
		var responseErr apiResponseError
		hasResponseErr := errors.As(err, &responseErr)
		if hasResponseErr && responseErr.ErrorBody.Code == "CHAT_STEER_UNCERTAIN" {
			if recoverOnly {
				return fmt.Errorf("%w; delivery handle %s remains unresolved and was not resent", err, clientMessageID)
			}
			hostArg := ""
			if c.targetsRemote() {
				// Recovery must ask the same daemon; the session ID means
				// nothing (or another session) on this machine.
				hostArg = " --host " + shellQuote(strings.TrimSpace(c.hostSelector))
			}
			return fmt.Errorf(
				"%w; recover this delivery without resending it: ao send%s --session %s --steer --recover-only --client-message-id %s",
				err, hostArg, strings.TrimPrefix(sessionPath, "sessions/"), clientMessageID)
		}
		if errors.Is(err, errDaemonUnavailable) || !hasResponseErr || responseErr.StatusCode >= 500 {
			return fmt.Errorf(
				"%w; outcome is unknown for delivery handle %s; retry safely with --client-message-id %s",
				err, clientMessageID, clientMessageID)
		}
		return err
	}

	if result.Outcome == "steered" {
		if recoverOnly {
			_, _ = fmt.Fprintf(c.deps.Out,
				"Recovered steering receipt for turn %s with delivery handle %s; agent action is not confirmed.\n",
				result.ProviderTurnID, clientMessageID)
			return nil
		}
		_, _ = fmt.Fprintf(c.deps.Out,
			"Steering accepted by provider for active turn %s with delivery handle %s; agent action is not confirmed.\n",
			result.ProviderTurnID, clientMessageID)
		return nil
	}
	if result.Duplicate {
		_, _ = fmt.Fprintln(c.deps.Out, "Message was already accepted; delivery state is unchanged.")
		return nil
	}
	_, _ = fmt.Fprintf(c.deps.Out,
		"No active turn to steer. Message accepted as a normal Chat turn in %s state with delivery handle %s; provider delivery is not confirmed.\n",
		result.State, clientMessageID)
	return nil
}

package domain

import (
	"encoding/base64"
	"net/url"
	"strings"
)

const (
	reviewNudgeIDPrefix = "review-nudge:"
	reviewNudgeOpen     = "<ao-review-nudge "
	reviewNudgeClose    = "\n</ao-review-nudge>"
)

// ReviewNudgeToken names the review thread a queued nudge came from.
type ReviewNudgeToken struct {
	PRURL    string
	ThreadID string
}

// WrapReviewNudge attaches the token to a nudge body for the session manager.
func WrapReviewNudge(token ReviewNudgeToken, body string) string {
	return reviewNudgeOpen + "pr=" + url.QueryEscape(token.PRURL) +
		" thread=" + url.QueryEscape(token.ThreadID) + ">\n" + body + reviewNudgeClose
}

// ParseReviewNudge splits a wrapped nudge into its token and body.
func ParseReviewNudge(message string) (ReviewNudgeToken, string, bool) {
	rest, ok := strings.CutPrefix(message, reviewNudgeOpen)
	if !ok || !strings.HasSuffix(rest, reviewNudgeClose) {
		return ReviewNudgeToken{}, "", false
	}
	header, body, ok := strings.Cut(strings.TrimSuffix(rest, reviewNudgeClose), ">\n")
	if !ok {
		return ReviewNudgeToken{}, "", false
	}
	prField, threadField, ok := strings.Cut(header, " ")
	if !ok {
		return ReviewNudgeToken{}, "", false
	}
	pr, prOK := strings.CutPrefix(prField, "pr=")
	thread, threadOK := strings.CutPrefix(threadField, "thread=")
	if !prOK || !threadOK {
		return ReviewNudgeToken{}, "", false
	}
	prURL, prErr := url.QueryUnescape(pr)
	threadID, threadErr := url.QueryUnescape(thread)
	if prErr != nil || threadErr != nil {
		return ReviewNudgeToken{}, "", false
	}
	return ReviewNudgeToken{PRURL: prURL, ThreadID: threadID}, body, true
}

// ReviewNudgeClientMessageID carries the token through durable queues. unique
// stops a later nudge for the same thread deduplicating against this one.
func ReviewNudgeClientMessageID(token ReviewNudgeToken, unique string) string {
	return reviewNudgeIDPrefix + base64URL(token.PRURL) + ":" + base64URL(token.ThreadID) + ":" + unique
}

// ParseReviewNudgeClientMessageID recovers the token from a client message id.
func ParseReviewNudgeClientMessageID(clientMessageID string) (ReviewNudgeToken, bool) {
	rest, ok := strings.CutPrefix(clientMessageID, reviewNudgeIDPrefix)
	if !ok {
		return ReviewNudgeToken{}, false
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 3 {
		return ReviewNudgeToken{}, false
	}
	prURL, prErr := base64.RawURLEncoding.DecodeString(parts[0])
	threadID, threadErr := base64.RawURLEncoding.DecodeString(parts[1])
	if prErr != nil || threadErr != nil {
		return ReviewNudgeToken{}, false
	}
	return ReviewNudgeToken{PRURL: string(prURL), ThreadID: string(threadID)}, true
}

func base64URL(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

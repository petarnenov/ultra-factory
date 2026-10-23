package domain

import "testing"

func TestReviewNudgeEnvelopeRoundTrips(t *testing.T) {
	token := ReviewNudgeToken{PRURL: "https://github.com/o/r/pull/7", ThreadID: "PRRT_kw DOVL>x"}
	body := "line one\n\nline two >\n"

	got, gotBody, ok := ParseReviewNudge(WrapReviewNudge(token, body))

	if !ok || got != token || gotBody != body {
		t.Fatalf("ParseReviewNudge = %+v %q %v, want %+v %q true", got, gotBody, ok, token, body)
	}
}

func TestParseReviewNudgeRejectsPlainAndMalformedMessages(t *testing.T) {
	for _, message := range []string{
		"plain nudge",
		"<ao-review-nudge pr=a thread=b>\nbody without close",
		"<ao-review-nudge pr=a>\nbody\n</ao-review-nudge>",
		"<ao-review-nudge pr=%zz thread=b>\nbody\n</ao-review-nudge>",
	} {
		if _, _, ok := ParseReviewNudge(message); ok {
			t.Errorf("ParseReviewNudge(%q) accepted a malformed envelope", message)
		}
	}
}

func TestReviewNudgeClientMessageIDRoundTrips(t *testing.T) {
	token := ReviewNudgeToken{PRURL: "https://github.com/o/r/pull/7", ThreadID: "PRRT_kwDOVLQKmc6uvsOC"}

	got, ok := ParseReviewNudgeClientMessageID(ReviewNudgeClientMessageID(token, "launch-1"))

	if !ok || got != token {
		t.Fatalf("ParseReviewNudgeClientMessageID = %+v %v, want %+v true", got, ok, token)
	}
	if _, ok := ParseReviewNudgeClientMessageID("session-send:launch-1"); ok {
		t.Fatal("accepted an ordinary client message id")
	}
}

package domain

import (
	"strings"
	"testing"
	"time"
)

func TestFormatAgentMessageIncludesTimestampAndSender(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 34, 56, 0, time.FixedZone("test", 3*60*60))
	got := FormatAgentMessage("continue the task", "  worker-1\n", at)
	want := "[Sent 2026-10-23T09:34:56Z by worker-1]\n\ncontinue the task"
	if got != want {
		t.Fatalf("FormatAgentMessage() = %q, want %q", got, want)
	}
	if got := FormatAgentMessage("hello", "", at); !strings.Contains(got, " by user]") {
		t.Fatalf("empty sender header = %q, want user fallback", got)
	}
}

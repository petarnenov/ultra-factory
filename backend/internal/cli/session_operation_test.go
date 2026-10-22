package cli

import (
	"strings"
	"testing"
	"time"
)

func TestSessionAgentOperationDetails(t *testing.T) {
	if got := sessionAgentOperationDetails(nil); got != "" {
		t.Fatalf("idle details = %q, want empty", got)
	}
	op := &sessionAgentOperationDTO{Kind: "resume", StartedAt: time.Now().Add(-42 * time.Second)}
	got := sessionAgentOperationDetails(op)
	if !strings.HasPrefix(got, "resume in progress for ") || !strings.Contains(got, "(since ") {
		t.Fatalf("details = %q, want the operation name and its age", got)
	}
}

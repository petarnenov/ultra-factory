package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
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

func TestAgentSwitchListShowsTransientOperationWithoutSwitchRows(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	started := time.Date(2026, 10, 23, 9, 30, 0, 0, time.UTC)
	response := agentSwitchListResponse{Operation: &sessionAgentOperationDTO{
		Kind: "resume", StartedAt: started, RequestID: "wake-req-18",
	}}
	if err := writeAgentSwitchList(cmd, response); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "operation: resume in progress since 2026-10-23T09:30:00Z") ||
		!strings.Contains(got, "started by request wake-req-18") {
		t.Fatalf("output = %q, want transient operation and starter request", got)
	}
}

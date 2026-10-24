//go:build consultarchive_live

package consultations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

// TestRealArchiveScanner reads the approved local archive without mutating it.
func TestRealArchiveScanner(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Join(home, ".ao")
	name := "ultra-factory-issue-639-d418fb8-20261023T211546Z"
	path := filepath.Join(stateRoot, "architecture-consults/archive/2026-10-23", name, "fallback-arbitration/fable.md")
	evidence, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Skip("approved real archive is not present on this host")
	}
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(evidence)
	found := false
	store := &Store{Write: runnerFunc(func(_ context.Context, q string, p map[string]any) ([]knowledgegraph.Row, error) {
		if p["key"] != "real-archive-test/2026-10-23/"+name {
			return nil, nil
		}
		var s Summary
		if err := json.Unmarshal([]byte(p["summary"].(string)), &s); err != nil {
			t.Fatal(err)
		}
		if s.Requester != "" {
			t.Fatal("inferred requester from owner or request prose")
		}
		if s.Mode != "fallback_arbitration" || s.Arbitration.Status != "completed" || s.Rounds != 0 || s.ProposalVersions != 1 {
			t.Fatalf("incorrect arbitration summary %+v", s)
		}
		if s.Archive != "verified" || s.Delivery != "final_response_pending_unconfirmed" || len(s.ConsulClosure) != 4 {
			t.Fatal("lost recorded nested status")
		}
		for _, item := range p["artifacts"].([]map[string]any) {
			if item["name"] != "fallback-arbitration/fable.md" {
				continue
			}
			var a Artifact
			if err := json.Unmarshal([]byte(item["content"].(string)), &a); err != nil {
				t.Fatal(err)
			}
			if a.Content != string(evidence) || a.Hash != hex.EncodeToString(digest[:]) || a.Phase != "arbitration" || len(a.Ballots) != 0 {
				t.Fatal("lost or misclassified arbiter evidence")
			}
			found = true
		}
		return nil, nil
	})}
	scanner := &Scanner{Store: store, DataDir: filepath.Join(stateRoot, "data"), Host: "real-archive-test"}
	defer scanner.close()
	for pass := 0; pass < 32 && !found; pass++ {
		scanner.Scan(context.Background())
	}
	if !found {
		t.Fatal("canonical archive was not discovered from daemon dataDir")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(evidence) {
		t.Fatal("archive changed during scan")
	}
}

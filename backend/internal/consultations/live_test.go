//go:build kglive

package consultations

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

// TestLiveIndex is opt-in and removes only its own unique consultation nodes.
func TestLiveIndex(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := knowledgegraph.Load(os.Getenv, filepath.Join(home, ".ao/data"))
	if err != nil || !cfg.Enabled() {
		t.Skip("no graph configured")
	}
	cfg.Timeout = 10 * time.Second
	client := knowledgegraph.NewClient(cfg, nil)
	store := &Store{Write: client, Read: client}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := store.Schema(ctx); err != nil {
		t.Fatal(err)
	}
	dir, root := archive(t)
	put(t, dir, "request.md", "Original question")
	put(t, dir, "proposal-2.md", "Second proposal")
	host := "consultation-test-" + time.Now().UTC().Format("20060102T150405.000000000")
	detail, err := ReadArchive(ctx, root, host, "2026-10-23", "case", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key, _ := DecodeID(detail.ID)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := client.Run(cleanup, `MATCH (c:ArchitectureConsultation {key:$key}) OPTIONAL MATCH (c)-[:HAS_ARTIFACT]->(a) DETACH DELETE c,a`, map[string]any{"key": key})
		if err != nil {
			t.Error(err)
		}
	})
	for range 2 {
		if err := store.Import(ctx, detail); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.Detail(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Artifacts) != len(detail.Artifacts) || got.Rounds != 0 || got.ProposalVersions != 1 {
		t.Fatalf("duplicate or missing artifacts: %+v", got)
	}
	if err := os.Remove(filepath.Join(dir, "2026-10-23/case/proposal-2.md")); err != nil {
		t.Fatal(err)
	}
	updated, err := ReadArchive(ctx, root, host, "2026-10-23", "case", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Import(ctx, updated); err != nil {
		t.Fatal(err)
	}
	got, err = store.Detail(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range got.Artifacts {
		if a.Name == "proposal-2.md" {
			found = a.Missing && a.AbsentSince != "" && a.Content == "Second proposal"
		}
	}
	if !found {
		t.Fatal("vanished round evidence was not retained")
	}
}

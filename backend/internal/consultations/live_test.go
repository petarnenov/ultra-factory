//go:build kglive

package consultations

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/consultations/testgraph"
	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

func liveClient(t *testing.T) *testgraph.Target {
	t.Helper()
	client, err := testgraph.Open(os.Getenv, func(cfg knowledgegraph.Config) knowledgegraph.Runner {
		return knowledgegraph.NewClient(cfg, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Declared disposable graph %s: %s/db/%s; schema and fixture writes enabled", client.ID, client.URL, client.Database)
	return client
}

// TestLiveIndex is opt-in and removes only its own unique consultation nodes.
func TestLiveIndex(t *testing.T) {
	client := liveClient(t)
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

func TestLiveUnavailableEvidenceRetainsLastCopy(t *testing.T) {
	client := liveClient(t)
	store := &Store{Write: client, Read: client}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	dir, root := archive(t)
	put(t, dir, "request.md", "Retained question")
	host := "unavailable-test-" + time.Now().UTC().Format("20060102T150405.000000000")
	d, err := ReadArchive(ctx, root, host, "2026-10-23", "case", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key, _ := DecodeID(d.ID)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := client.Run(cleanup, `MATCH (c:ArchitectureConsultation {key:$key}) OPTIONAL MATCH (c)-[:HAS_ARTIFACT]->(a) DETACH DELETE c,a`, map[string]any{"key": key})
		if err != nil {
			t.Error(err)
		}
	})
	if err := store.Import(ctx, d); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "2026-10-23/case/request.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside-sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	d, err = ReadArchive(ctx, root, host, "2026-10-23", "case", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Import(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, err := store.Detail(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := got.Artifacts[0]
	if !a.Unavailable || a.Missing || a.AbsentSince != "" || a.Content != "Retained question" {
		t.Fatal("unsafe artifact overwrote or misreported retained copy")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "request.md", "Restored question")
	d, err = ReadArchive(ctx, root, host, "2026-10-23", "case", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Import(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, err = store.Detail(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	a = got.Artifacts[0]
	if a.Unavailable || a.Missing || a.Content != "Restored question" {
		t.Fatal("restored artifact stayed unavailable")
	}
}

func TestLiveCompleteOneRoundImportDetailNotPartial(t *testing.T) {
	client := liveClient(t)
	store := &Store{Write: client, Read: client}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	dir, root := archive(t)
	writeCompleteArchive(t, dir)
	host := "complete-round-test-" + time.Now().UTC().Format("20060102T150405.000000000")
	d, err := ReadArchive(ctx, root, host, "2026-10-23", "case", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key, _ := DecodeID(d.ID)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := client.Run(cleanup, `MATCH (c:ArchitectureConsultation {key:$key}) OPTIONAL MATCH (c)-[:HAS_ARTIFACT]->(a) DETACH DELETE c,a`, map[string]any{"key": key})
		if err != nil {
			t.Error(err)
		}
	})
	if d.Partial {
		t.Fatal("source fixture is not complete")
	}
	if err := store.Import(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, err := store.Detail(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Partial || got.Rounds != 1 || got.ProposalVersions != 1 || len(got.Artifacts) != len(d.Artifacts) {
		t.Fatalf("unused placeholders marked complete consultation partial: %+v", got.Summary)
	}
}

func TestLiveUnavailableRoundEvidenceImportDetail(t *testing.T) {
	client := liveClient(t)
	store := &Store{Write: client, Read: client}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, directory := range []bool{false, true} {
		name := "proposal"
		if directory {
			name = "ballot directory"
		}
		t.Run(name, func(t *testing.T) {
			dir, root := archive(t)
			artifactName := "proposal-2.md"
			replaceName := artifactName
			expectedRounds := 0
			if directory {
				artifactName = "ballots/round-2/astra.md"
				replaceName = "ballots/round-2"
				expectedRounds = 1
			}
			original := "Original proposal"
			if directory {
				original = "Proposal: proposal-2.md\nD639-1: APPROVE"
			}
			put(t, dir, artifactName, original)
			host := "round-state-test-" + time.Now().UTC().Format("20060102T150405.000000000")
			d, err := ReadArchive(ctx, root, host, "2026-10-23", "case", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			key, _ := DecodeID(d.ID)
			t.Cleanup(func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, err := client.Run(cleanup, `MATCH (c:ArchitectureConsultation {key:$key}) OPTIONAL MATCH (c)-[:HAS_ARTIFACT]->(a) DETACH DELETE c,a`, map[string]any{"key": key})
				if err != nil {
					t.Error(err)
				}
			})
			if err := store.Import(ctx, d); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "2026-10-23/case", replaceName)
			if err := os.Rename(path, path+".original"); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if !directory {
				outside = filepath.Join(outside, "outside.md")
				if err := os.WriteFile(outside, []byte("outside-sentinel"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			d, err = ReadArchive(ctx, root, host, "2026-10-23", "case", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Import(ctx, d); err != nil {
				t.Fatal(err)
			}
			got, err := store.Detail(ctx, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, a := range got.Artifacts {
				if a.Name == artifactName {
					found = a.Unavailable && !a.Missing && a.AbsentSince == "" && a.Content == original && a.Hash != ""
				}
			}
			if !found || !got.Partial || got.Rounds != expectedRounds {
				t.Fatalf("round state was lost or invented: %+v", got.Summary)
			}
		})
	}
}

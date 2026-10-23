package consultations

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

type runnerFunc func(context.Context, string, map[string]any) ([]knowledgegraph.Row, error)

func (f runnerFunc) Run(ctx context.Context, q string, p map[string]any) ([]knowledgegraph.Row, error) {
	return f(ctx, q, p)
}
func TestStoreAtomicUpsertAndReads(t *testing.T) {
	dir, root := archive(t)
	put(t, dir, "final.md", "Decision")
	d, _ := ReadArchive(context.Background(), root, "h", "2026-10-23", "case", time.Now())
	calls := 0
	var params map[string]any
	writer := runnerFunc(func(_ context.Context, q string, p map[string]any) ([]knowledgegraph.Row, error) {
		calls++
		if !strings.Contains(q, "MERGE") || strings.Contains(q, d.ID) {
			t.Fatal("not parameterized")
		}
		params = p
		return nil, nil
	})
	s := &Store{Write: writer}
	for range 2 {
		if err := s.Import(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 || params["key"] != "h/2026-10-23/case" {
		t.Fatal("unstable identity")
	}
	summary := params["summary"]
	s.Read = runnerFunc(func(ctx context.Context, q string, p map[string]any) ([]knowledgegraph.Row, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded read")
		}
		if strings.Contains(q, "RETURN c.key") {
			if p["limit"] != 2 {
				t.Fatal(p)
			}
			return []knowledgegraph.Row{{"summary": summary}, {"summary": summary}}, nil
		}
		rows := []knowledgegraph.Row{}
		for _, a := range d.Artifacts {
			raw, _ := json.Marshal(a)
			rows = append(rows, knowledgegraph.Row{"summary": summary, "artifact": string(raw), "absentSince": "2026-10-24", "warnings": []any{"partial"}})
		}
		return rows, nil
	})
	page, err := s.List(context.Background(), 1, "")
	if err != nil || len(page.Items) != 1 || page.NextCursor != d.ID {
		t.Fatal(page, err)
	}
	got, err := s.Detail(context.Background(), d.ID)
	if err != nil || len(got.Artifacts) != len(d.Artifacts) || !got.Partial || got.Artifacts[0].AbsentSince == "" {
		t.Fatal(got, err)
	}
	if _, err := s.List(context.Background(), 1, page.NextCursor); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 51} {
		if _, err := s.List(context.Background(), limit, ""); err == nil {
			t.Fatal("invalid limit")
		}
	}
	if _, err := s.List(context.Background(), 1, "!"); err == nil {
		t.Fatal("invalid cursor")
	}
	if _, err := s.Detail(context.Background(), "!"); err == nil {
		t.Fatal("invalid ID")
	}
	if err := s.Import(context.Background(), Detail{}); err == nil {
		t.Fatal("invalid import")
	}
	s.Read = runnerFunc(func(context.Context, string, map[string]any) ([]knowledgegraph.Row, error) { return nil, nil })
	if _, err := s.Detail(context.Background(), d.ID); !errors.Is(err, ErrMissing) {
		t.Fatal(err)
	}
	s.Read = runnerFunc(func(context.Context, string, map[string]any) ([]knowledgegraph.Row, error) {
		return []knowledgegraph.Row{{"summary": "{"}}, nil
	})
	if _, err := s.List(context.Background(), 1, ""); err == nil {
		t.Fatal("bad summary")
	}
	if _, err := s.Detail(context.Background(), d.ID); err == nil {
		t.Fatal("bad summary")
	}
	s.Read = runnerFunc(func(context.Context, string, map[string]any) ([]knowledgegraph.Row, error) {
		return nil, errors.New("offline")
	})
	if _, err := s.List(context.Background(), 1, ""); err == nil {
		t.Fatal("offline")
	}
	if _, err := s.Detail(context.Background(), d.ID); err == nil {
		t.Fatal("offline")
	}
}
func TestScannerCatchUpAndRetry(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, "architecture-consults/archive")
	for i := range 40 {
		path := filepath.Join(archiveDir, "2026-10-23", strings.Repeat("a", i+1))
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	fail := true
	imports := 0
	schema := 0
	s := &Store{Write: runnerFunc(func(_ context.Context, q string, _ map[string]any) ([]knowledgegraph.Row, error) {
		if fail {
			return nil, errors.New("offline")
		}
		if strings.HasPrefix(q, "CREATE") {
			schema++
		} else if strings.Contains(q, "ArchitectureConsultation {key:") {
			imports++
		}
		return nil, nil
	})}
	scanner := &Scanner{Store: s, DataDir: filepath.Join(dir, "data"), Host: "host"}
	defer scanner.close()
	scanner.Scan(context.Background())
	if s.Status().Error == "" {
		t.Fatal("missing scan failure")
	}
	fail = false
	scanner.Scan(context.Background())
	if imports == 0 || imports > 32 || schema != 5 {
		t.Fatal(imports, schema)
	}
	first := imports
	scanner.Scan(context.Background())
	if imports <= first {
		t.Fatal("no catchup")
	}
	fail = true
	scanner.Scan(context.Background())
	if s.Status().Error == "" {
		t.Fatal("missing retry failure")
	}
	fail = false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scanner.Run(ctx)
	empty := &Scanner{Store: s, DataDir: filepath.Join(t.TempDir(), "data"), Host: "host"}
	empty.Scan(context.Background())
	empty.close()
	if s.Status().Error != "" {
		t.Fatal("missing archive treated as failure")
	}
	unsafe := t.TempDir()
	if err := os.Symlink(archiveDir, filepath.Join(unsafe, "architecture-consults")); err != nil {
		t.Fatal(err)
	}
	empty = &Scanner{Store: s, DataDir: filepath.Join(unsafe, "data"), Host: "host"}
	empty.Scan(context.Background())
	empty.close()
	if s.Status().Error == "" {
		t.Fatal("unsafe root")
	}
}

func TestImportFingerprintIgnoresScanClock(t *testing.T) {
	dir, root := archive(t)
	put(t, dir, "final.md", "Evidence")
	d, err := ReadArchive(context.Background(), root, "h", "2026-10-23", "case", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var fingerprints []string
	s := &Store{Write: runnerFunc(func(_ context.Context, _ string, p map[string]any) ([]knowledgegraph.Row, error) {
		fingerprints = append(fingerprints, p["digest"].(string))
		return nil, nil
	})}
	if err := s.Import(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	d.SyncedAt = "2026-10-24T00:00:00Z"
	if err := s.Import(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if fingerprints[0] != fingerprints[1] {
		t.Fatal("scan clock changed content digest")
	}
	d.Artifacts[0].Missing = !d.Artifacts[0].Missing
	if err := s.Import(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if fingerprints[1] == fingerprints[2] {
		t.Fatal("missing evidence did not change digest")
	}
}

func TestDetailDistinguishesUnavailableRetainedEvidence(t *testing.T) {
	summary, _ := json.Marshal(Summary{ID: EncodeID("h/2026-10-23/case")})
	artifact, _ := json.Marshal(Artifact{Name: "request.md", Phase: "request", Content: "Last indexed question", Hash: "full-hash", Ballots: []Ballot{}})
	s := &Store{Read: runnerFunc(func(context.Context, string, map[string]any) ([]knowledgegraph.Row, error) {
		return []knowledgegraph.Row{{"summary": string(summary), "artifact": string(artifact), "missing": false, "unavailable": true}}, nil
	})}
	d, err := s.Detail(context.Background(), EncodeID("h/2026-10-23/case"))
	if err != nil {
		t.Fatal(err)
	}
	a := d.Artifacts[0]
	if !d.Partial || !a.Unavailable || a.Missing || a.AbsentSince != "" || a.Content != "Last indexed question" {
		t.Fatal("unavailability destroyed or misreported evidence")
	}
}

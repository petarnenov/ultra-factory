//go:build kglive

package knowledgegraph

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveGraph runs the queries against a real graph configured in
// ~/.ao/knowledge-graph.json: go test -tags kglive ./internal/knowledgegraph/
func TestLiveGraph(t *testing.T) {
	cfg, err := Load(os.Getenv)
	if err != nil || !cfg.Enabled() {
		t.Skipf("no knowledge graph configured: %v", err)
	}
	cfg.Timeout = 15 * time.Second
	c := NewClient(cfg, nil)
	g := NewGraph(c)
	ctx := context.Background()
	const project = "__kglive_test"
	t.Cleanup(func() {
		_, _ = c.Run(ctx, "MATCH (n) WHERE n.project = $p OR n.id = $p DETACH DELETE n", map[string]any{"p": project})
		_, _ = c.Run(ctx, "MATCH (t:Topic) WHERE NOT (t)--() DELETE t", nil)
	})
	now := time.Now()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(g.RecordTask(ctx, TaskFact{Key: "h/a1", SessionID: "a1", Host: "h", Project: project, Title: "Fix sqlite migration ordering", Issue: "42", Outcome: "failed", Report: "stuck: duplicate goose migration number", At: now}))
	must(g.AddLesson(ctx, Lesson{ID: "kglive-L1", TaskKey: "h/a1", Session: "a1", Host: "h", Project: project, Kind: "pitfall", Text: "Sqlite migration numbers must be unique", Topics: []string{"sqlite"}, At: now}))
	must(g.MarkServed(ctx, "h/a2", []string{"kglive-L1"}, now))
	must(g.RecordTask(ctx, TaskFact{Key: "h/a2", SessionID: "a2", Host: "h", Project: project, Title: "Add sqlite migration for reports", Issue: "43", Outcome: "merged", At: now}))
	// A second write of the same outcome must not score the lesson twice.
	must(g.RecordTask(ctx, TaskFact{Key: "h/a2", SessionID: "a2", Host: "h", Project: project, Outcome: "merged", At: now}))
	rows, err := c.Run(ctx, "MATCH (l:Lesson {id:'kglive-L1'}) RETURN l.helped AS helped, l.hurt AS hurt", nil)
	must(err)
	if len(rows) != 1 || rows[0].Float("helped") != 1 || rows[0].Float("hurt") != 0 {
		t.Fatalf("lesson score = %v, want helped 1 hurt 0", rows)
	}
	items, err := g.Prior(ctx, Query{Project: project, Issue: "42", Text: "new sqlite migration table", ExcludeTask: "h/none"})
	must(err)
	sections := map[string]int{}
	for _, it := range items {
		sections[it.Section]++
	}
	if sections["attempt"] != 1 || sections["lesson"] != 1 || sections["task"] != 1 {
		t.Fatalf("sections = %v, items %+v", sections, items)
	}
	out := FormatSection(items)
	for _, want := range []string{"Earlier attempts", "Sqlite migration numbers", "Add sqlite migration for reports"} {
		if !strings.Contains(out, want) {
			t.Fatalf("section misses %q:\n%s", want, out)
		}
	}
}

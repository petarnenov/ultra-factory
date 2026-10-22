package session

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type priorCall struct {
	project domain.ProjectID
	issue   domain.IssueID
	text    string
	hasDL   bool
}

type servedCall struct {
	id  domain.SessionID
	ids []string
}

type fakeKnowledge struct {
	mu      sync.Mutex
	section string
	ids     []string
	err     error
	markErr error
	prior   []priorCall
	served  []servedCall
}

func (f *fakeKnowledge) PriorContext(ctx context.Context, project domain.ProjectID, issue domain.IssueID, text string) (string, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, hasDL := ctx.Deadline()
	f.prior = append(f.prior, priorCall{project: project, issue: issue, text: text, hasDL: hasDL})
	return f.section, f.ids, f.err
}

func (f *fakeKnowledge) MarkServed(_ context.Context, id domain.SessionID, lessonIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.served = append(f.served, servedCall{id: id, ids: lessonIDs})
	return f.markErr
}

func TestWithKnowledgeContextSkips(t *testing.T) {
	tests := []struct {
		name      string
		knowledge bool
		cfg       ports.SpawnConfig
	}{
		{name: "no knowledge graph", cfg: ports.SpawnConfig{Prompt: "fix it"}},
		{name: "orchestrator kind", knowledge: true, cfg: ports.SpawnConfig{Kind: domain.KindOrchestrator, Prompt: "fix it"}},
		{name: "empty prompt and issue", knowledge: true, cfg: ports.SpawnConfig{Kind: domain.KindWorker, Prompt: "  \n"}},
		{name: "existing context kept", knowledge: true, cfg: ports.SpawnConfig{Prompt: "fix it", KnowledgeContext: "given"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &fakeKnowledge{section: "## Knowledge Graph", ids: []string{"l1"}}
			svc := &Service{}
			if tt.knowledge {
				svc.SetKnowledgeGraph(k)
			}

			got, ids := svc.withKnowledgeContext(context.Background(), tt.cfg, domain.ProjectRecord{ID: "ao"})

			if !reflect.DeepEqual(got, tt.cfg) || ids != nil {
				t.Fatalf("cfg = %+v ids = %v, want unchanged", got, ids)
			}
			if len(k.prior) != 0 {
				t.Fatalf("PriorContext called %d times, want 0", len(k.prior))
			}
		})
	}
}

func TestWithKnowledgeContextAddsSection(t *testing.T) {
	tests := []struct {
		name string
		cfg  ports.SpawnConfig
	}{
		{name: "unset kind defaults to worker", cfg: ports.SpawnConfig{Prompt: "fix flaky test", IssueContext: "Issue body", IssueID: "42"}},
		{name: "issue without prompt", cfg: ports.SpawnConfig{Kind: domain.KindWorker, IssueID: "42", IssueContext: "Issue body"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &fakeKnowledge{section: "## Knowledge Graph\n\nlesson", ids: []string{"l1", "l2"}}
			svc := &Service{}
			svc.SetKnowledgeGraph(k)

			got, ids := svc.withKnowledgeContext(context.Background(), tt.cfg, domain.ProjectRecord{ID: "ao"})

			if got.KnowledgeContext != k.section {
				t.Fatalf("KnowledgeContext = %q, want section", got.KnowledgeContext)
			}
			if !reflect.DeepEqual(ids, []string{"l1", "l2"}) {
				t.Fatalf("ids = %v", ids)
			}
			want := priorCall{project: "ao", issue: "42", text: tt.cfg.Prompt + "\n" + tt.cfg.IssueContext, hasDL: true}
			if len(k.prior) != 1 || k.prior[0] != want {
				t.Fatalf("prior calls = %+v, want %+v", k.prior, want)
			}
		})
	}
}

func TestWithKnowledgeContextErrorLeavesConfig(t *testing.T) {
	var logs bytes.Buffer
	k := &fakeKnowledge{section: "ignored", ids: []string{"l1"}, err: errors.New("graph down")}
	svc := &Service{logger: slog.New(slog.NewTextHandler(&logs, nil))}
	svc.SetKnowledgeGraph(k)
	cfg := ports.SpawnConfig{Prompt: "fix it"}

	got, ids := svc.withKnowledgeContext(context.Background(), cfg, domain.ProjectRecord{ID: "ao"})

	if !reflect.DeepEqual(got, cfg) || ids != nil {
		t.Fatalf("cfg = %+v ids = %v, want unchanged", got, ids)
	}
	if !strings.Contains(logs.String(), "prior context unavailable") {
		t.Fatalf("logs = %q, want warning", logs.String())
	}
}

func TestMarkKnowledgeServed(t *testing.T) {
	tests := []struct {
		name      string
		knowledge bool
		ids       []string
		wantCalls int
	}{
		{name: "no knowledge graph", ids: []string{"l1"}},
		{name: "no lesson ids", knowledge: true},
		{name: "marks lessons in background", knowledge: true, ids: []string{"l1"}, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &fakeKnowledge{}
			ran := 0
			svc := &Service{backgroundContext: context.Background(), runBackground: func(work func()) { ran++; work() }}
			if tt.knowledge {
				svc.SetKnowledgeGraph(k)
			}

			svc.markKnowledgeServed("s1", tt.ids)

			if ran != tt.wantCalls || len(k.served) != tt.wantCalls {
				t.Fatalf("background runs = %d served = %d, want %d", ran, len(k.served), tt.wantCalls)
			}
			if tt.wantCalls == 1 && !reflect.DeepEqual(k.served[0], servedCall{id: "s1", ids: tt.ids}) {
				t.Fatalf("served = %+v", k.served[0])
			}
		})
	}
}

func TestMarkKnowledgeServedLogsFailure(t *testing.T) {
	var logs bytes.Buffer
	k := &fakeKnowledge{markErr: errors.New("graph down")}
	svc := &Service{backgroundContext: context.Background(), runBackground: runInline, logger: slog.New(slog.NewTextHandler(&logs, nil))}
	svc.SetKnowledgeGraph(k)

	svc.markKnowledgeServed("s1", []string{"l1"})

	if !strings.Contains(logs.String(), "mark served lessons failed") {
		t.Fatalf("logs = %q, want warning", logs.String())
	}
}

func TestMarkKnowledgeServedWithoutRunnerUsesGoroutine(t *testing.T) {
	k := &fakeKnowledge{}
	svc := &Service{backgroundContext: context.Background()}
	svc.SetKnowledgeGraph(k)

	svc.markKnowledgeServed("s1", []string{"l1"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		k.mu.Lock()
		n := len(k.served)
		k.mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("MarkServed was not called from the background goroutine")
}

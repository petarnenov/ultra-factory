package taskreport

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

var t0 = time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

type fakeSessions struct{ list []domain.Session }

func (f fakeSessions) List(_ context.Context, filter sessionsvc.ListFilter) ([]domain.Session, error) {
	var out []domain.Session
	for _, s := range f.list {
		if s.ProjectID == filter.ProjectID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f fakeSessions) Get(_ context.Context, id domain.SessionID) (domain.Session, error) {
	for _, s := range f.list {
		if s.ID == id {
			return s, nil
		}
	}
	return domain.Session{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
}

type fakeStore struct {
	transitions map[domain.SessionID][]domain.StageTransition
	config      map[domain.SessionID][]domain.ConfigChange
	usage       map[domain.SessionID][]domain.UsageRun
}

func (f fakeStore) GetProject(_ context.Context, id string) (domain.ProjectRecord, bool, error) {
	return domain.ProjectRecord{ID: id}, id == "ao", nil
}

func (f fakeStore) ListSessionStageTransitions(_ context.Context, id domain.SessionID) ([]domain.StageTransition, error) {
	return f.transitions[id], nil
}

func (f fakeStore) ListSessionConfigHistory(_ context.Context, id domain.SessionID) ([]domain.ConfigChange, error) {
	return f.config[id], nil
}

func (f fakeStore) ListSessionUsageRuns(_ context.Context, id domain.SessionID) ([]domain.UsageRun, error) {
	return f.usage[id], nil
}

func worker(id string, created time.Time) domain.Session {
	return domain.Session{SessionRecord: domain.SessionRecord{
		ID: domain.SessionID(id), ProjectID: "ao", Kind: domain.KindWorker,
		Harness: domain.HarnessClaudeCode, CreatedAt: created,
		Metadata: domain.SessionMetadata{Model: "claude-opus-5-5", Effort: "medium"},
	}}
}

func TestClassifyOutcomes(t *testing.T) {
	merged := worker("ao-1", t0)
	merged.PRs = []domain.PRFacts{{URL: "u1", Merged: true}, {URL: "u2", Closed: true}}
	closed := worker("ao-2", t0)
	closed.PRs = []domain.PRFacts{{URL: "u3", Closed: true}}
	openLive := worker("ao-3", t0)
	openLive.PRs = []domain.PRFacts{{URL: "u4"}}
	unfinished := worker("ao-4", t0)
	unfinished.IsTerminated = true
	unfinished.PRs = []domain.PRFacts{{URL: "u5"}}
	noPR := worker("ao-7", t0)
	noPR.IsTerminated = true
	failed := worker("ao-5", t0)
	failed.IsTerminated = true
	failed.ProvisionState = domain.SessionProvisionFailed
	live := worker("ao-6", t0)

	for _, tc := range []struct {
		sess domain.Session
		want Outcome
		ok   bool
	}{
		{merged, OutcomeMerged, true},
		{closed, OutcomeClosed, true},
		{openLive, "", false},
		{unfinished, OutcomeUnfinished, true},
		{noPR, OutcomeNoPR, true},
		{failed, OutcomeFailed, true},
		{live, "", false},
	} {
		got, ok := classify(tc.sess)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: classify = %q,%v want %q,%v", tc.sess.ID, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSummarizeCarriesPRDisplayFacts(t *testing.T) {
	sess := worker("ao-1", t0)
	sess.IsTerminated = true
	sess.PRs = []domain.PRFacts{{
		URL: "https://github.com/o/r/pull/7", Number: 7, Title: "feat: add completed-task cards",
		Additions: 120, Deletions: 8, ChangedFiles: 5,
	}}
	outcome, ok := classify(sess)
	if !ok || outcome != OutcomeUnfinished {
		t.Fatalf("classify = %q,%v", outcome, ok)
	}
	got := summarize(sess, outcome, nil)
	if len(got.PRs) != 1 {
		t.Fatalf("prs = %+v", got.PRs)
	}
	pr := got.PRs[0]
	if pr.Title != "feat: add completed-task cards" || pr.Additions != 120 || pr.Deletions != 8 || pr.ChangedFiles != 5 {
		t.Fatalf("pr display facts = %+v", pr)
	}
}

func TestListFiltersSortsAndPages(t *testing.T) {
	a := worker("ao-1", t0)
	a.PRs = []domain.PRFacts{{URL: "a", Number: 1, Merged: true, StateChangedAt: t0.Add(2 * time.Hour)}}
	b := worker("ao-2", t0)
	b.PRs = []domain.PRFacts{{URL: "b", Number: 2, Merged: true, StateChangedAt: t0.Add(3 * time.Hour)}}
	c := worker("ao-3", t0.Add(time.Hour))
	c.IsTerminated = true
	orch := worker("ao-4", t0)
	orch.Kind = domain.KindOrchestrator
	orch.IsTerminated = true
	other := worker("x-1", t0)
	other.ProjectID = "other"
	other.IsTerminated = true
	svc := New(Deps{
		Sessions: fakeSessions{list: []domain.Session{a, b, c, orch, other}},
		Store: fakeStore{usage: map[domain.SessionID][]domain.UsageRun{
			"ao-2": {{BindingState: domain.UsageBindingComplete, ModelID: "m", EventCount: 2,
				InputTokens: 100, KnownInputCount: 2, CachedInputTokens: 40, KnownCachedInputCount: 2,
				OutputTokens: 7, KnownOutputCount: 2}},
		}},
	})
	ctx := context.Background()

	page, err := svc.List(ctx, "ao", ListInput{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tasks) != 2 || page.Tasks[0].SessionID != "ao-2" || page.Tasks[1].SessionID != "ao-1" || page.NextCursor != "2" {
		t.Fatalf("page 1 = %+v", page)
	}
	first := page.Tasks[0]
	if first.CompletedAt == nil || *first.ElapsedSeconds != int64(3*time.Hour/time.Second) {
		t.Fatalf("completion = %+v", first)
	}
	if first.Tokens.Completeness != Complete || *first.Tokens.InputTokens != 100 || *first.Tokens.CachedInputTokens != 40 || *first.Tokens.OutputTokens != 7 {
		t.Fatalf("tokens = %+v", first.Tokens)
	}
	if page.Tasks[1].Tokens.Completeness != Unavailable || page.Tasks[1].Tokens.InputTokens != nil {
		t.Fatalf("missing usage must be unavailable, got %+v", page.Tasks[1].Tokens)
	}

	page, err = svc.List(ctx, "ao", ListInput{Limit: 2, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tasks) != 1 || page.Tasks[0].SessionID != "ao-3" || page.Tasks[0].CompletedAt != nil ||
		page.Tasks[0].ElapsedSeconds != nil || page.NextCursor != "" {
		t.Fatalf("page 2 = %+v", page)
	}

	page, err = svc.List(ctx, "ao", ListInput{Outcome: OutcomeNoPR})
	if err != nil || len(page.Tasks) != 1 || page.Tasks[0].Outcome != OutcomeNoPR {
		t.Fatalf("filtered = %+v, %v", page, err)
	}

	for _, in := range []ListInput{{Outcome: "done"}, {Cursor: "x"}} {
		var apiErr *apierr.Error
		if _, err := svc.List(ctx, "ao", in); !errors.As(err, &apiErr) || apiErr.Kind != apierr.KindInvalid {
			t.Errorf("%+v: err = %v, want invalid", in, err)
		}
	}
	var apiErr *apierr.Error
	if _, err := svc.List(ctx, "missing", ListInput{}); !errors.As(err, &apiErr) || apiErr.Kind != apierr.KindNotFound {
		t.Errorf("missing project err = %v", err)
	}
}

func TestGetBuildsReportFromRecordedFacts(t *testing.T) {
	sess := worker("ao-1", t0)
	sess.IsTerminated = true
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	store := fakeStore{
		transitions: map[domain.SessionID][]domain.StageTransition{"ao-1": {
			{Stage: domain.KanbanBuilding, EnteredAt: at(0)},
			{Stage: domain.KanbanValidating, EnteredAt: at(10)},
			{Stage: domain.KanbanBuilding, EnteredAt: at(15)},
			{Stage: domain.KanbanValidating, EnteredAt: at(25)},
			{Stage: domain.KanbanArchive, EnteredAt: at(30)},
		}},
		config: map[domain.SessionID][]domain.ConfigChange{"ao-1": {
			{Source: domain.ConfigChangeSession, Harness: "claude-code", Model: "claude-opus-5-5", Effort: "medium", RecordedAt: at(0)},
			{Source: domain.ConfigChangeConversation, Harness: "claude-code", Effort: "high", RecordedAt: at(12)},
			{Source: domain.ConfigChangeConversation, Harness: "claude-code", RecordedAt: at(20)},
		}},
		usage: map[domain.SessionID][]domain.UsageRun{"ao-1": {
			{BindingID: 1, BindingState: domain.UsageBindingComplete, ModelID: "claude-opus-5-5", EventCount: 3,
				InputTokens: 50, KnownInputCount: 3, OutputTokens: 9, KnownOutputCount: 3},
			{BindingID: 2, BindingState: domain.UsageBindingPartial, ModelID: "claude-opus-5-5", EventCount: 2,
				InputTokens: 10, KnownInputCount: 1, OutputTokens: 1, KnownOutputCount: 1},
		}},
	}
	svc := New(Deps{Sessions: fakeSessions{list: []domain.Session{sess}}, Store: store, Now: func() time.Time { return at(60) }})
	report, err := svc.Get(context.Background(), "ao", "ao-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != OutcomeNoPR || report.CompletedAt == nil || !report.CompletedAt.Equal(at(30)) || *report.ElapsedSeconds != 1800 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	if report.Stages.Completeness != Complete || len(report.Stages.Visits) != 4 {
		t.Fatalf("stages = %+v", report.Stages)
	}
	totals := map[domain.KanbanColumn]StageTotal{}
	for _, total := range report.Stages.Totals {
		totals[total.Stage] = total
	}
	if totals[domain.KanbanBuilding].Seconds != 1200 || totals[domain.KanbanBuilding].Visits != 2 ||
		totals[domain.KanbanValidating].Seconds != 600 || totals[domain.KanbanValidating].Visits != 2 {
		t.Fatalf("totals = %+v", totals)
	}
	segs := report.Config.Segments
	if report.Config.Completeness != Complete || len(segs) != 3 || segs[1].Effort != "high" || segs[1].Model != "claude-opus-5-5" || segs[2].Effort != "medium" {
		t.Fatalf("config = %+v", report.Config)
	}
	if report.Tokens.Completeness != Partial || *report.Tokens.InputTokens != 60 || report.Tokens.CachedInputTokens != nil ||
		report.Runs[0].Completeness != Complete || report.Runs[1].Completeness != Partial {
		t.Fatalf("usage = %+v runs = %+v", report.Tokens, report.Runs)
	}
}

func TestGetReportsMissingHistoryAsUnavailableOrPartial(t *testing.T) {
	sess := worker("ao-1", t0)
	sess.PRs = []domain.PRFacts{{URL: "u", Merged: true}}
	late := t0.Add(2 * time.Hour)
	store := fakeStore{
		transitions: map[domain.SessionID][]domain.StageTransition{},
		config: map[domain.SessionID][]domain.ConfigChange{"ao-1": {
			{Source: domain.ConfigChangeSession, Model: "m", RecordedAt: late},
		}},
	}
	svc := New(Deps{Sessions: fakeSessions{list: []domain.Session{sess}}, Store: store})
	report, err := svc.Get(context.Background(), "ao", "ao-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.CompletedAt != nil || report.ElapsedSeconds != nil {
		t.Fatalf("merge time was never recorded, got %+v", report.Summary)
	}
	if report.Stages.Completeness != Unavailable || len(report.Stages.Totals) != 0 {
		t.Fatalf("stages = %+v", report.Stages)
	}
	if report.Config.Completeness != Partial {
		t.Fatalf("config = %+v", report.Config)
	}
	if report.Tokens.Completeness != Unavailable || report.Tokens.InputTokens != nil || report.Tokens.OutputTokens != nil {
		t.Fatalf("tokens = %+v", report.Tokens)
	}

	open := stageHistory(t0, nil, []domain.StageTransition{{Stage: domain.KanbanReady, EnteredAt: t0}}, t0.Add(time.Minute))
	if open.Completeness != Partial || open.Visits[0].LeftAt != nil || !open.Totals[0].HasUnfinished || open.Totals[0].Seconds != 60 {
		t.Fatalf("open visit = %+v", open)
	}
}

func TestGetRejectsTasksOutsideProjectOrIncomplete(t *testing.T) {
	live := worker("ao-1", t0)
	foreign := worker("x-1", t0)
	foreign.ProjectID = "other"
	foreign.IsTerminated = true
	svc := New(Deps{Sessions: fakeSessions{list: []domain.Session{live, foreign}}, Store: fakeStore{}})
	for _, id := range []domain.SessionID{"ao-1", "x-1"} {
		var apiErr *apierr.Error
		if _, err := svc.Get(context.Background(), "ao", id); !errors.As(err, &apiErr) || apiErr.Code != "COMPLETED_TASK_NOT_FOUND" {
			t.Errorf("%s: err = %v", id, err)
		}
	}
}

func TestGetCapsStageVisitsAtCompletion(t *testing.T) {
	merged := t0.Add(90 * time.Minute)
	sess := worker("ao-1", t0)
	sess.PRs = []domain.PRFacts{{URL: "u", Merged: true, StateChangedAt: merged}}
	store := fakeStore{transitions: map[domain.SessionID][]domain.StageTransition{"ao-1": {
		{Stage: domain.KanbanBuilding, EnteredAt: t0},
		{Stage: domain.KanbanNeedsReview, EnteredAt: t0.Add(time.Hour)},
		{Stage: domain.KanbanReady, EnteredAt: t0.Add(80 * time.Minute)},
		{Stage: domain.KanbanBuilding, EnteredAt: t0.Add(5 * time.Hour)},
	}}}
	svc := New(Deps{Sessions: fakeSessions{list: []domain.Session{sess}}, Store: store, Now: func() time.Time { return t0.Add(30 * 24 * time.Hour) }})
	report, err := svc.Get(context.Background(), "ao", "ao-1")
	if err != nil {
		t.Fatal(err)
	}
	stages := report.Stages
	if stages.Completeness != Complete || len(stages.Visits) != 3 {
		t.Fatalf("stages = %+v", stages)
	}
	last := stages.Visits[2]
	if last.Stage != domain.KanbanReady || last.LeftAt == nil || !last.LeftAt.Equal(merged) || last.DurationSeconds != 600 {
		t.Fatalf("ready visit = %+v", last)
	}
	for _, total := range stages.Totals {
		if total.HasUnfinished {
			t.Fatalf("completed task reported as ongoing: %+v", total)
		}
		if total.Stage == domain.KanbanBuilding && (total.Visits != 1 || total.Seconds != 3600) {
			t.Fatalf("post-completion visit counted: %+v", total)
		}
	}
}

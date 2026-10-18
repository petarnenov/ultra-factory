package report

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

type fakeStore struct {
	session domain.SessionRecord
	ok      bool
	created domain.ReportRecord
	reports []domain.ReportRecord
}

func (f *fakeStore) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	return f.session, f.ok, nil
}
func (f *fakeStore) CreateReport(_ context.Context, r domain.ReportRecord) (domain.ReportRecord, error) {
	f.created = r
	return r, nil
}
func (f *fakeStore) ListReportsByProject(context.Context, domain.ProjectID) ([]domain.ReportRecord, error) {
	return append([]domain.ReportRecord(nil), f.reports...), nil
}

func TestListProjectIsReadOnlyAndOrdered(t *testing.T) {
	at := time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC)
	outputs := []domain.ReportOutput{
		{Kind: domain.ReportOutputArtifact, Reference: "first"},
		{Kind: domain.ReportOutputArtifact, Reference: "second"},
	}
	st := &fakeStore{reports: []domain.ReportRecord{
		{ID: "c", CreatedAt: at.Add(time.Second)},
		{ID: "b", CreatedAt: at, Outputs: outputs},
		{ID: "a", CreatedAt: at},
	}}
	got, err := New(Deps{Store: st}).ListProject(context.Background(), "ao")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "c" || got[1].ID != "b" || got[2].ID != "a" {
		t.Fatalf("report order = %+v", got)
	}
	if len(got[1].Outputs) != 2 || got[1].Outputs[0].Reference != "first" || got[1].Outputs[1].Reference != "second" {
		t.Fatalf("output position order = %+v", got[1].Outputs)
	}
}

func TestListProjectAddsLocalAgentAndPreservesRemoteIdentity(t *testing.T) {
	st := &fakeStore{
		ok:      true,
		session: domain.SessionRecord{ID: "same-id", Harness: "codex", DisplayName: "local worker"},
		reports: []domain.ReportRecord{
			{ID: "local", SessionID: "same-id"},
			{ID: "remote", SessionID: "same-id", HostID: "host-remote", HostLabel: "build box", Agent: "claude-code", SessionDisplayName: "remote worker"},
		},
	}
	got, err := New(Deps{Store: st}).ListProject(context.Background(), "ao")
	if err != nil {
		t.Fatal(err)
	}
	if got[1].Agent != "codex" || got[1].SessionDisplayName != "local worker" {
		t.Fatalf("local source = %+v", got[1])
	}
	if got[0].HostID != "host-remote" || got[0].HostLabel != "build box" || got[0].Agent != "claude-code" || got[0].SessionDisplayName != "remote worker" {
		t.Fatalf("remote source was overwritten by colliding local session: %+v", got[0])
	}
}

func TestCreateDerivesOwnershipAndFixedDeliveryDeadlines(t *testing.T) {
	now := time.Date(2026, 9, 7, 1, 2, 3, 0, time.UTC)
	tests := []struct {
		name      string
		input     CreateInput
		available time.Time
		settles   time.Time
	}{
		{name: "free form batches for one hour", input: CreateInput{Message: "status"}, available: now.Add(time.Hour)},
		{name: "output only batches for one hour", input: CreateInput{Outputs: []domain.ReportOutput{{Kind: domain.ReportOutputArtifact, Reference: "result"}}}, available: now.Add(time.Hour)},
		{name: "checkpoint batches for one hour", input: CreateInput{State: domain.ReportCheckpoint, Note: "checkpoint"}, available: now.Add(time.Hour)},
		{name: "needs input is immediate", input: CreateInput{State: domain.ReportNeedsInput, Note: "decision"}, available: now},
		{name: "stuck is immediate", input: CreateInput{State: domain.ReportStuck, Note: "blocked"}, available: now},
		{name: "done settles for five minutes", input: CreateInput{State: domain.ReportDone, Note: "finished"}, available: now.Add(5 * time.Minute), settles: now.Add(5 * time.Minute)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &fakeStore{ok: true, session: domain.SessionRecord{ID: "ao-7", ProjectID: "ao", Kind: domain.KindWorker}}
			s := New(Deps{Store: st, Now: func() time.Time { return now }, NewID: func() string { return "rpt_1" }})
			tc.input.SessionID = "ao-7"
			r, err := s.Create(context.Background(), tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if r.ID != "rpt_1" || r.ProjectID != "ao" || r.DeliveryState != domain.ReportPending || r.RepeatCount != 1 || !r.AvailableAt.Equal(tc.available) || !r.SettlementDeadline.Equal(tc.settles) {
				t.Fatalf("report=%+v", r)
			}
		})
	}
}

func TestCreateDefendsValidationAndOwnership(t *testing.T) {
	worker := &fakeStore{ok: true, session: domain.SessionRecord{ID: "ao-2", ProjectID: "ao", Kind: domain.KindWorker}}
	for _, tc := range []struct {
		name  string
		store *fakeStore
		input CreateInput
	}{
		{"missing session", worker, CreateInput{Message: "message"}},
		{"unknown session", &fakeStore{}, CreateInput{SessionID: "ao-2", State: domain.ReportDone, Note: "done"}},
		{"orchestrator", &fakeStore{ok: true, session: domain.SessionRecord{ID: "ao-1", ProjectID: "ao", Kind: domain.KindOrchestrator}}, CreateInput{SessionID: "ao-1", State: domain.ReportDone, Note: "done"}},
		{"bad state", worker, CreateInput{SessionID: "ao-2", State: "bad", Note: "note"}},
		{"bad PR", worker, CreateInput{SessionID: "ao-2", Outputs: []domain.ReportOutput{{Kind: domain.ReportOutputPRCreated, Reference: "https://example.com/x"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(Deps{Store: tc.store}).Create(context.Background(), tc.input)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

type artifactCalls []string

func (a *artifactCalls) RecordReportedArtifact(_ context.Context, id domain.SessionID, reference string) {
	*a = append(*a, string(id)+" "+reference)
}

func TestCreateShowsEachReportedArtifactAfterTheReportIsStored(t *testing.T) {
	st := &fakeStore{ok: true, session: domain.SessionRecord{ID: "ao-7", ProjectID: "ao", Kind: domain.KindWorker}}
	var calls artifactCalls
	r, err := New(Deps{Store: st, Artifacts: &calls}).Create(context.Background(), CreateInput{
		SessionID: "ao-7", State: domain.ReportDone, Note: "done",
		Outputs: []domain.ReportOutput{
			{Kind: domain.ReportOutputArtifact, Reference: "report.html"},
			{Kind: domain.ReportOutputPRCreated, Reference: "https://github.com/o/r/pull/1"},
			{Kind: domain.ReportOutputArtifact, Reference: "/abs/notes.md"},
		},
	})
	if err != nil || r.ID == "" || st.created.ID != r.ID {
		t.Fatalf("report = %+v, %v", r, err)
	}
	if len(calls) != 2 || calls[0] != "ao-7 report.html" || calls[1] != "ao-7 /abs/notes.md" {
		t.Fatalf("recorded = %q, want the two artifacts in order", calls)
	}

	// A refused report shows nothing.
	calls = nil
	if _, err := New(Deps{Store: &fakeStore{}, Artifacts: &calls}).Create(context.Background(), CreateInput{
		SessionID: "ao-7", Outputs: []domain.ReportOutput{{Kind: domain.ReportOutputArtifact, Reference: "report.html"}},
	}); err == nil || len(calls) != 0 {
		t.Fatalf("unknown session: err = %v, recorded %q", err, calls)
	}
}

func TestListProjectPageWalksCursorAcrossStableTies(t *testing.T) {
	at := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	st := &fakeStore{
		ok:      true,
		session: domain.SessionRecord{ID: "same-id", Harness: "codex", DisplayName: "local worker"},
		reports: []domain.ReportRecord{
			{ID: "local-newer", SessionID: "same-id", CreatedAt: at.Add(time.Minute)},
			{ID: "local-a", SessionID: "same-id", CreatedAt: at},
			{ID: "remote-b", SessionID: "same-id", HostID: "host-remote", HostLabel: "build box", CreatedAt: at},
		},
	}
	s := New(Deps{Store: st})
	first, err := s.ListProjectPage(context.Background(), "ao", ListProjectFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Reports) != 2 || first.Reports[0].ID != "local-newer" || first.Reports[1].ID != "remote-b" || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	second, err := s.ListProjectPage(context.Background(), "ao", ListProjectFilter{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Reports) != 1 || second.Reports[0].ID != "local-a" || second.NextCursor != "" {
		t.Fatalf("second page = %+v", second)
	}
	seen := map[string]bool{}
	for _, page := range [][]domain.ReportRecord{first.Reports, second.Reports} {
		for _, record := range page {
			if seen[record.ID] {
				t.Fatalf("duplicate report %s", record.ID)
			}
			seen[record.ID] = true
		}
	}
}

func TestListProjectPageUsesSelfHostNameAndKeepsRemoteLabel(t *testing.T) {
	at := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	st := &fakeStore{
		ok:      true,
		session: domain.SessionRecord{ID: "worker", Harness: "codex", DisplayName: "local worker"},
		reports: []domain.ReportRecord{
			{ID: "local", SessionID: "worker", CreatedAt: at},
			{ID: "remote", SessionID: "worker", HostID: "host-remote", HostLabel: "build box", CreatedAt: at},
		},
	}
	s := New(Deps{Store: st, SelfHostID: "h_local", SelfHostName: "devbox"})
	page, err := s.ListProjectPage(context.Background(), "ao", ListProjectFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.ReportRecord{}
	for _, report := range page.Reports {
		byID[report.ID] = report
	}
	if byID["local"].HostLabel != "devbox" {
		t.Fatalf("local host label = %q", byID["local"].HostLabel)
	}
	if byID["remote"].HostLabel != "build box" {
		t.Fatalf("remote host label = %q", byID["remote"].HostLabel)
	}
}

func TestListProjectPageRejectsMalformedCursor(t *testing.T) {
	st := &fakeStore{reports: []domain.ReportRecord{{ID: "r", CreatedAt: time.Now().UTC()}}}
	_, err := New(Deps{Store: st}).ListProjectPage(context.Background(), "ao", ListProjectFilter{Cursor: "not-base64!!!"})
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "INVALID_REPORT_CURSOR" {
		t.Fatalf("err = %v, want INVALID_REPORT_CURSOR", err)
	}
}

// A standalone worker has no project and so no orchestrator to receive its
// report: Create rejects it as invalid input, not as an internal error.
func TestCreateRejectsAWorkerWithoutAProject(t *testing.T) {
	st := &fakeStore{ok: true, session: domain.SessionRecord{ID: "standalone-5", Kind: domain.KindWorker}}
	_, err := New(Deps{Store: st}).Create(context.Background(), CreateInput{SessionID: "standalone-5", Message: "hi"})
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Kind != apierr.KindInvalid || apiErr.Code != "REPORT_PROJECT_REQUIRED" {
		t.Fatalf("err = %v, want invalid REPORT_PROJECT_REQUIRED", err)
	}
	if st.created.ID != "" {
		t.Fatalf("a report was stored: %+v", st.created)
	}
}

package report

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

type pullStore struct {
	fakeStore
	bySession []domain.ReportRecord
	ackSeen   []string
	ackToken  string
}

func (p *pullStore) ListReportsBySession(context.Context, domain.SessionID) ([]domain.ReportRecord, error) {
	return append([]domain.ReportRecord(nil), p.bySession...), nil
}

func (p *pullStore) AcknowledgePendingSessionReports(_ context.Context, _ domain.SessionID, ids []string, token string, _ time.Time) ([]string, error) {
	p.ackSeen = append(p.ackSeen, ids...)
	p.ackToken = token
	return ids[:1], nil
}

func apiCode(err error) string {
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

func TestListDuePendingReturnsOnlyPendingReportsThatAreDue(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	st := &pullStore{fakeStore: fakeStore{ok: true, session: domain.SessionRecord{ID: "w", Kind: domain.KindWorker}}}
	st.bySession = []domain.ReportRecord{
		{ID: "due", DeliveryState: domain.ReportPending, AvailableAt: now},
		{ID: "settling-done", DeliveryState: domain.ReportPending, AvailableAt: now.Add(time.Minute)},
		{ID: "claimed", DeliveryState: domain.ReportClaimed, AvailableAt: now.Add(-time.Hour)},
		{ID: "acked", DeliveryState: domain.ReportAcknowledged, AvailableAt: now.Add(-time.Hour)},
		{ID: "overdue", DeliveryState: domain.ReportPending, AvailableAt: now.Add(-time.Minute)},
	}
	svc := New(Deps{Store: st, Now: func() time.Time { return now }})
	got, more, err := svc.ListDuePending(context.Background(), "w")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "due" || got[1].ID != "overdue" {
		t.Fatalf("due pending = %+v", got)
	}
	if !more {
		t.Fatal("a done report inside its settlement window must be reported as more pending")
	}

	st.bySession = st.bySession[:1]
	if _, more, _ := svc.ListDuePending(context.Background(), "w"); more {
		t.Fatal("more pending reported with nothing left")
	}
}

func TestListDuePendingCapsOnePullAtOneAcknowledgement(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	st := &pullStore{fakeStore: fakeStore{ok: true, session: domain.SessionRecord{ID: "w", Kind: domain.KindWorker}}}
	for i := 0; i < MaxAcknowledgeReports+5; i++ {
		st.bySession = append(st.bySession, domain.ReportRecord{ID: fmt.Sprintf("rpt_%03d", i), DeliveryState: domain.ReportPending, AvailableAt: now})
	}
	got, more, err := New(Deps{Store: st, Now: func() time.Time { return now }}).ListDuePending(context.Background(), "w")
	if err != nil || len(got) != MaxAcknowledgeReports || !more {
		t.Fatalf("len=%d more=%v err=%v", len(got), more, err)
	}
}

func TestPullRoutesRequireAnExistingWorker(t *testing.T) {
	cases := []struct {
		name  string
		store *pullStore
		id    domain.SessionID
		code  string
	}{
		{"missing id", &pullStore{fakeStore: fakeStore{ok: true}}, "", "INVALID_REPORT_SESSION"},
		{"unknown session", &pullStore{}, "w", "SESSION_NOT_FOUND"},
		{"orchestrator", &pullStore{fakeStore: fakeStore{ok: true, session: domain.SessionRecord{ID: "o", Kind: domain.KindOrchestrator}}}, "o", "REPORT_WORKER_REQUIRED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := New(Deps{Store: tc.store})
			if _, _, err := svc.ListDuePending(context.Background(), tc.id); apiCode(err) != tc.code {
				t.Fatalf("ListDuePending err = %v, want %s", err, tc.code)
			}
			if _, err := svc.AcknowledgePulled(context.Background(), tc.id, []string{"rpt"}); apiCode(err) != tc.code {
				t.Fatalf("AcknowledgePulled err = %v, want %s", err, tc.code)
			}
			if len(tc.store.ackSeen) != 0 {
				t.Fatal("acknowledged reports of a session that is not a worker")
			}
		})
	}
}

func TestAcknowledgePulledValidatesIDsAndUsesAPulledToken(t *testing.T) {
	worker := fakeStore{ok: true, session: domain.SessionRecord{ID: "w", Kind: domain.KindWorker}}
	tooMany := make([]string, MaxAcknowledgeReports+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("rpt_%d", i)
	}
	for name, ids := range map[string][]string{"none": nil, "blank": {"rpt_1", " "}, "too many": tooMany} {
		st := &pullStore{fakeStore: worker}
		if _, err := New(Deps{Store: st}).AcknowledgePulled(context.Background(), "w", ids); apiCode(err) != "INVALID_REPORT_IDS" {
			t.Fatalf("%s: err = %v", name, err)
		}
		if len(st.ackSeen) != 0 {
			t.Fatalf("%s: invalid input reached the store", name)
		}
	}
	st := &pullStore{fakeStore: worker}
	got, err := New(Deps{Store: st}).AcknowledgePulled(context.Background(), "w", []string{"rpt_1", "rpt_2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "rpt_1" || !strings.HasPrefix(st.ackToken, "pulled:") {
		t.Fatalf("acknowledged = %v token = %q", got, st.ackToken)
	}
}

func TestDeliverExternalSubmitsToTheActiveOrchestratorWithHostAttribution(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	store := newCoordinatorStore(now)
	delivery := &coordinatorDelivery{}
	c := NewCoordinator(CoordinatorDeps{Store: store, Delivery: delivery, Now: func() time.Time { return now }})
	reports := []domain.ReportRecord{{
		ID: "rpt_remote", SessionID: "widgets-3", ProjectID: "p", SessionDisplayName: "auth refactor", HostLabel: "build-box",
		State: domain.ReportDone, Note: "PR ready", CreatedAt: now, RepeatCount: 1,
		Outputs: []domain.ReportOutput{{Kind: domain.ReportOutputPRCreated, Reference: "https://github.com/o/r/pull/9"}},
	}}

	if err := c.DeliverExternal(context.Background(), "p", reports); !errors.Is(err, ErrNoActiveOrchestrator) {
		t.Fatalf("without an orchestrator err = %v", err)
	}
	if len(delivery.submissions) != 0 {
		t.Fatal("submitted without an orchestrator")
	}

	store.sessions = []domain.SessionRecord{
		{ID: "old", ProjectID: "p", Kind: domain.KindOrchestrator, IsTerminated: true, CreatedAt: now},
		{ID: "respawned", ProjectID: "p", Kind: domain.KindOrchestrator, CreatedAt: now.Add(time.Second)},
	}
	if err := c.DeliverExternal(context.Background(), "p", reports); err != nil {
		t.Fatal(err)
	}
	if len(delivery.submissions) != 1 || delivery.submissions[0].target != "respawned" {
		t.Fatalf("submissions = %#v", delivery.submissions)
	}
	msg := delivery.submissions[0].message
	if !strings.Contains(msg, "[done] auth refactor (session widgets-3 on host build-box)") || strings.Contains(msg, "ao://") ||
		!strings.Contains(msg, "PR ready") || !strings.Contains(msg, "pr_created: https://github.com/o/r/pull/9") {
		t.Fatalf("rendered = %q", msg)
	}
	if delivery.submissions[0].key != (PreparedBatch{Reports: reports}).IdempotencyKey() {
		t.Fatal("idempotency key is not derived from the pulled report IDs")
	}

	delivery.submitErr = errors.New("busy")
	if err := c.DeliverExternal(context.Background(), "p", reports); err == nil {
		t.Fatal("a refused submission must surface so the caller does not acknowledge")
	}
}

func TestStuckCoalescingKeepsWorkersOnDifferentHostsApart(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	stuck := func(id, host string) domain.ReportRecord {
		return domain.ReportRecord{ID: id, SessionID: "widgets-3", HostLabel: host, State: domain.ReportStuck, Note: "blocked", CreatedAt: now, RepeatCount: 1}
	}
	rendered := PreparedBatch{Reports: []domain.ReportRecord{stuck("a", "linux"), stuck("b", "imac"), stuck("c", "linux")}}.Render()
	if !strings.Contains(rendered, "on host linux) (repeated 2 times)") || strings.Count(rendered, "[stuck]") != 2 {
		t.Fatalf("rendered = %q", rendered)
	}
}

func TestListDuePendingKeepsOneResponseWithinItsByteBudget(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	big := func(id string, size int) domain.ReportRecord {
		return domain.ReportRecord{ID: id, DeliveryState: domain.ReportPending, AvailableAt: now,
			Outputs: []domain.ReportOutput{{Kind: domain.ReportOutputArtifact, Reference: strings.Repeat("x", size)}}}
	}
	st := &pullStore{fakeStore: fakeStore{ok: true, session: domain.SessionRecord{ID: "w", Kind: domain.KindWorker}}}
	st.bySession = []domain.ReportRecord{big("a", 1<<20), big("b", 1<<20), big("c", 10)}
	svc := New(Deps{Store: st, Now: func() time.Time { return now }})
	got, more, err := svc.ListDuePending(context.Background(), "w")
	// b does not fit, and c must not overtake it: the page stops at a.
	if err != nil || len(got) != 1 || got[0].ID != "a" || !more {
		t.Fatalf("got %d reports more=%v err=%v", len(got), more, err)
	}
	st.bySession = []domain.ReportRecord{big("huge", 3<<20)}
	if got, _, _ := svc.ListDuePending(context.Background(), "w"); len(got) != 1 {
		t.Fatal("a single report over the budget must still be listed so it can be settled")
	}
}

func TestListDuePendingBudgetsTheEncodedSizeNotTheRawText(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	st := &pullStore{fakeStore: fakeStore{ok: true, session: domain.SessionRecord{ID: "w", Kind: domain.KindWorker}}}
	for i := 0; i < 8; i++ {
		// 100 KiB of '&' is 600 KiB once JSON-escaped as \u0026.
		st.bySession = append(st.bySession, domain.ReportRecord{ID: fmt.Sprintf("r%d", i), DeliveryState: domain.ReportPending, AvailableAt: now,
			Outputs: []domain.ReportOutput{{Kind: domain.ReportOutputArtifact, Reference: "https://x?" + strings.Repeat("&", 100<<10)}}})
	}
	got, more, err := New(Deps{Store: st, Now: func() time.Time { return now }}).ListDuePending(context.Background(), "w")
	if err != nil || !more || len(got) == 0 || len(got) > 3 {
		t.Fatalf("listed %d reports (more=%v): the encoded page would exceed the budget", len(got), more)
	}
}

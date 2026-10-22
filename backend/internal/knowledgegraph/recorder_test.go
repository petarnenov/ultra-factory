package knowledgegraph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/taskreport"
)

// syncBuffer is a log sink safe for the recorder goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func queued(s *Service) []job {
	var out []job
	for {
		select {
		case j := <-s.queue:
			out = append(out, j)
		default:
			return out
		}
	}
}

func TestReportCreated(t *testing.T) {
	tests := []struct {
		name  string
		rec   domain.ReportRecord
		wants []job
	}{
		{name: "checkpoint ignored", rec: domain.ReportRecord{SessionID: "s1", State: domain.ReportCheckpoint, Note: "n"}},
		{name: "empty state ignored", rec: domain.ReportRecord{SessionID: "s1", Note: "n"}},
		{name: "done joins note and message", rec: domain.ReportRecord{SessionID: "s1", State: domain.ReportDone, Note: " PR open ", Message: "all green"}, wants: []job{{session: "s1", report: "done: PR open — all green"}}},
		{name: "stuck note only", rec: domain.ReportRecord{SessionID: "s1", State: domain.ReportStuck, Note: "blocked", Message: "  "}, wants: []job{{session: "s1", report: "stuck: blocked"}}},
		{name: "needs input message only", rec: domain.ReportRecord{SessionID: "s1", State: domain.ReportNeedsInput, Message: "which db?"}, wants: []job{{session: "s1", report: "needs_input: which db?"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(&fakeRunner{}, &fakeSessions{}, nil)

			svc.ReportCreated(tt.rec)

			if got := queued(svc); !reflect.DeepEqual(got, tt.wants) {
				t.Fatalf("queued = %#v, want %#v", got, tt.wants)
			}
		})
	}
}

func TestSubscribeQueuesTerminatedSessions(t *testing.T) {
	b := cdc.NewBroadcaster()
	svc := newTestService(&fakeRunner{}, &fakeSessions{}, nil)
	unsubscribe := svc.Subscribe(b)
	payload := func(terminated bool) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{"isTerminated": terminated, "kind": "worker"})
		return raw
	}

	b.Publish(cdc.Event{Seq: 1, Type: cdc.EventSessionCreated, SessionID: "a", Payload: payload(true)})
	b.Publish(cdc.Event{Seq: 2, Type: cdc.EventSessionUpdated, SessionID: "", Payload: payload(true)})
	b.Publish(cdc.Event{Seq: 3, Type: cdc.EventSessionUpdated, SessionID: "b", Payload: payload(false)})
	b.Publish(cdc.Event{Seq: 4, Type: cdc.EventSessionUpdated, SessionID: "c", Payload: json.RawMessage(`{`)})
	b.Publish(cdc.Event{Seq: 5, Type: cdc.EventSessionUpdated, SessionID: "d", Payload: payload(true)})
	unsubscribe()
	b.Publish(cdc.Event{Seq: 6, Type: cdc.EventSessionUpdated, SessionID: "e", Payload: payload(true)})

	if got := queued(svc); !reflect.DeepEqual(got, []job{{session: "d"}}) {
		t.Fatalf("queued = %#v, want only d", got)
	}
	if b.SubscriberCount() != 0 {
		t.Fatalf("unsubscribe left %d subscribers", b.SubscriberCount())
	}
}

func TestEnqueueDropsWhenQueueFull(t *testing.T) {
	logs := &syncBuffer{}
	svc := New(Deps{Graph: NewGraph(&fakeRunner{}), Sessions: &fakeSessions{}, Logger: slog.New(slog.NewTextHandler(logs, nil))})
	for i := 0; i < queueSize; i++ {
		svc.enqueue(job{session: "fill"})
	}
	done := make(chan struct{})

	go func() {
		svc.ReportCreated(domain.ReportRecord{SessionID: "late", State: domain.ReportDone, Note: "n"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("ReportCreated blocked on a full queue")
	}
	if !strings.Contains(logs.String(), "queue full") || !strings.Contains(logs.String(), "session=late") {
		t.Fatalf("logs = %q, want queue full warning for late", logs.String())
	}
	if len(svc.queue) != queueSize {
		t.Fatalf("queue len = %d, want %d", len(svc.queue), queueSize)
	}
}

// startRecorder runs the recorder and stops it when the test ends.
func startRecorder(t *testing.T, svc *Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := svc.Start(ctx)
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("recorder did not stop")
		}
	})
}

func waitRun(t *testing.T, r *fakeRunner) runCall {
	t.Helper()
	select {
	case c := <-r.ran:
		return c
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for a graph write")
		return runCall{}
	}
}

func TestStartSkipsNonRecordableSessions(t *testing.T) {
	orch := worker("orch", "ao")
	orch.Kind = domain.KindOrchestrator
	sessions := &fakeSessions{sessions: map[domain.SessionID]domain.Session{
		"orch":      orch,
		"noproject": worker("noproject", ""),
		"sentinel":  worker("sentinel", "ao"),
	}}
	r := &fakeRunner{ran: make(chan runCall, 8)}
	svc := newTestService(r, sessions, nil)
	startRecorder(t, svc)

	svc.enqueue(job{session: "orch"})
	svc.enqueue(job{session: "noproject"})
	svc.enqueue(job{session: "missing"})
	svc.enqueue(job{session: "sentinel"})

	call := waitRun(t, r)
	if call.params["key"] != "host1/sentinel" {
		t.Fatalf("first write = %v, want sentinel only", call.params["key"])
	}
	if n := len(r.snapshot()); n != 1 {
		t.Fatalf("writes = %d, want 1", n)
	}
}

func TestStartEnrichesFromTaskReport(t *testing.T) {
	sess := worker("s1", "ao")
	sess.DisplayName = "session title"
	sess.IssueID = "#42"
	sess.Harness = "claude-code"
	sess.IsTerminated = true
	sess.Metadata.Model = "opus"
	sess.Metadata.Effort = "high"
	sess.PRs = []domain.PRFacts{{URL: "https://gh/pr/1"}, {URL: ""}}
	elapsed, in, out := int64(90), int64(1000), int64(200)
	var rep taskreport.Report
	rep.Outcome = taskreport.OutcomeMerged
	rep.Title = "Report title"
	rep.ElapsedSeconds = &elapsed
	rep.Tokens.InputTokens = &in
	rep.Tokens.OutputTokens = &out
	rep.Stages.Totals = []taskreport.StageTotal{
		{Stage: domain.KanbanBuilding, Seconds: 60, Visits: 1},
		{Stage: "validating", Seconds: 30, Visits: 2},
	}
	r := &fakeRunner{ran: make(chan runCall, 8)}
	svc := newTestService(r, &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": sess}},
		&fakeTasks{reports: map[domain.SessionID]taskreport.Report{"s1": rep}})
	startRecorder(t, svc)

	svc.enqueue(job{session: "s1"})

	p := waitRun(t, r).params
	want := map[string]any{
		"title": "Report title", "issue": "#42", "outcome": "merged", "harness": "claude-code",
		"model": "opus", "effort": "high", "stages": "building 60s x1, validating 30s x2",
		"prs": []string{"https://gh/pr/1"}, "elapsedSeconds": int64(90),
		"inputTokens": int64(1000), "outputTokens": int64(200),
	}
	if !reflect.DeepEqual(p["props"], want) {
		t.Fatalf("props = %#v, want %#v", p["props"], want)
	}
	if p["key"] != "host1/s1" || p["host"] != "host1" || p["issueKey"] != "ao/42" {
		t.Fatalf("params = %#v", p)
	}
}

func TestStartFallbackOutcome(t *testing.T) {
	live := worker("live", "ao")
	live.DisplayName = "kept title"
	dead := worker("dead", "ao")
	dead.IsTerminated = true
	tests := []struct {
		name        string
		session     domain.Session
		tasks       TaskReports
		wantOutcome string
	}{
		{name: "running without reports service", session: live, tasks: nil, wantOutcome: "in_progress"},
		{name: "running and not classified", session: live, tasks: &fakeTasks{}, wantOutcome: "in_progress"},
		{name: "terminated and not classified", session: dead, tasks: &fakeTasks{}, wantOutcome: "terminated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{ran: make(chan runCall, 8)}
			svc := newTestService(r, &fakeSessions{sessions: map[domain.SessionID]domain.Session{tt.session.ID: tt.session}}, tt.tasks)
			startRecorder(t, svc)

			svc.enqueue(job{session: tt.session.ID})

			props := waitRun(t, r).params["props"].(map[string]any)
			if props["outcome"] != tt.wantOutcome {
				t.Fatalf("outcome = %v, want %s", props["outcome"], tt.wantOutcome)
			}
			if _, ok := props["stages"]; ok {
				t.Fatalf("unclassified task must not carry stages")
			}
		})
	}
}

func TestStartDedupesRepeatedTerminations(t *testing.T) {
	dead := worker("s1", "ao")
	dead.IsTerminated = true
	r := &fakeRunner{ran: make(chan runCall, 8)}
	svc := newTestService(r, &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": dead}}, &fakeTasks{})
	startRecorder(t, svc)

	svc.enqueue(job{session: "s1"})
	svc.enqueue(job{session: "s1"})
	svc.enqueue(job{session: "s1", report: "done: ok"})
	svc.enqueue(job{session: "s1", report: "done: ok"})

	first := waitRun(t, r)
	second := waitRun(t, r)
	third := waitRun(t, r)
	if first.params["report"] != "" || second.params["report"] != "done: ok" || third.params["report"] != "done: ok" {
		t.Fatalf("reports = %v, %v, %v", first.params["report"], second.params["report"], third.params["report"])
	}
	if n := len(r.snapshot()); n != 3 {
		t.Fatalf("writes = %d, want 3 (duplicate termination skipped)", n)
	}
}

func TestStartRecordsNewOutcomeAfterTermination(t *testing.T) {
	dead := worker("s1", "ao")
	dead.IsTerminated = true
	var rep taskreport.Report
	rep.Outcome = taskreport.OutcomeClosed
	tasks := &fakeTasks{reports: map[domain.SessionID]taskreport.Report{}}
	r := &fakeRunner{ran: make(chan runCall, 8)}
	svc := newTestService(r, &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": dead}}, tasks)
	startRecorder(t, svc)

	svc.enqueue(job{session: "s1"})
	waitRun(t, r)
	tasks.reports["s1"] = rep
	svc.enqueue(job{session: "s1"})

	props := waitRun(t, r).params["props"].(map[string]any)
	if props["outcome"] != "closed" {
		t.Fatalf("outcome = %v, want closed", props["outcome"])
	}
}

func TestStartLogsFailures(t *testing.T) {
	tests := []struct {
		name     string
		sessErr  error
		runErr   error
		wantLogs string
	}{
		{name: "session read failure", sessErr: errors.New("db locked"), wantLogs: "db locked"},
		{name: "graph write failure", runErr: errors.New("graph down"), wantLogs: "graph down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &syncBuffer{}
			r := &fakeRunner{err: tt.runErr}
			svc := New(Deps{
				Graph: NewGraph(r), HostID: "host1",
				Sessions: &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": worker("s1", "ao")}, err: tt.sessErr},
				Logger:   slog.New(slog.NewTextHandler(logs, nil)),
			})
			startRecorder(t, svc)

			svc.enqueue(job{session: "s1", report: "done: x"})

			deadline := time.Now().Add(2 * time.Second)
			for !strings.Contains(logs.String(), "record task failed") && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			if got := logs.String(); !strings.Contains(got, "record task failed") || !strings.Contains(got, tt.wantLogs) {
				t.Fatalf("logs = %q, want failure with %q", got, tt.wantLogs)
			}
		})
	}
}

func TestStartStopsOnCancel(t *testing.T) {
	svc := newTestService(&fakeRunner{}, &fakeSessions{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := svc.Start(ctx)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Start did not close its channel after cancel")
	}
}

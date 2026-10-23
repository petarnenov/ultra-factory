package knowledgegraph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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
	sess.Metadata.Branch = "ao/s1-fix"
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
	sess.PRs = append(sess.PRs, domain.PRFacts{TargetBranch: "dev"})
	svc := newTestService(r, &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": sess}},
		&fakeTasks{reports: map[domain.SessionID]taskreport.Report{"s1": rep}})
	svc.paths = fixedChangedPaths{paths: []string{"backend/a.go", "backend/b.go"}}
	startRecorder(t, svc)

	svc.enqueue(job{session: "s1", report: "done: complete"})

	p := waitRun(t, r).params
	want := map[string]any{
		"failures": []string{},
		"title":    "Report title", "issue": "#42", "outcome": "merged", "harness": "claude-code",
		"model": "opus", "effort": "high", "branch": "ao/s1-fix", "stages": "building 60s x1, validating 30s x2",
		"prs": []string{"https://gh/pr/1"}, "files": []string{"backend/a.go", "backend/b.go"}, "elapsedSeconds": int64(90),
		"inputTokens": int64(1000), "outputTokens": int64(200),
	}
	if !reflect.DeepEqual(p["props"], want) {
		t.Fatalf("props = %#v, want %#v", p["props"], want)
	}
	if p["key"] != "host1/s1" || p["host"] != "host1" || p["issueKey"] != "ao/42" {
		t.Fatalf("params = %#v", p)
	}
}

func TestReportCaptureCapsPathsAndPreservesWhenWorkspaceMissing(t *testing.T) {
	sess := worker("s1", "ao")
	sess.PRs = []domain.PRFacts{{TargetBranch: "dev"}}
	paths := make([]string, maxChangedPaths+5)
	for i := range paths {
		paths[i] = fmt.Sprintf("file-%02d.go", i)
	}
	runner := &fakeRunner{ran: make(chan runCall, 4)}
	svc := newTestService(runner, &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": sess}}, nil)
	svc.paths = fixedChangedPaths{paths: paths}
	if err := svc.record(context.Background(), job{session: "s1", report: "done: first"}); err != nil {
		t.Fatal(err)
	}
	p := runner.snapshot()[0].params
	props := p["props"].(map[string]any)
	if got := props["files"].([]string); len(got) != maxChangedPaths || got[0] != "file-00.go" {
		t.Fatalf("captured files = %v (%d), want first %d paths", got, len(got), maxChangedPaths)
	}
	if !strings.Contains(runner.snapshot()[0].statement, "size($files) = 0") {
		t.Fatal("record query does not preserve stored paths when capture is empty")
	}
}

func TestChangedPathCaptureFailureDoesNotReplaceStoredPaths(t *testing.T) {
	sess := worker("s1", "ao")
	sess.PRs = []domain.PRFacts{{TargetBranch: "dev"}}
	runner := &fakeRunner{}
	svc := newTestService(runner, &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": sess}}, nil)
	svc.paths = fixedChangedPaths{err: errors.New("missing worktree")}
	if err := svc.record(context.Background(), job{session: "s1", report: "done: first"}); err != nil {
		t.Fatal(err)
	}
	params := runner.snapshot()[0].params
	if got := params["files"]; got == nil || len(got.([]string)) != 0 {
		t.Fatalf("files parameter = %#v, want empty to preserve previous value", got)
	}
}

func TestChangedPathsAreCapturedOnlyForReportJobs(t *testing.T) {
	sess := worker("s1", "ao")
	sess.PRs = []domain.PRFacts{{TargetBranch: "dev"}}
	paths := &pathCallSpy{}
	runner := &fakeRunner{}
	svc := newTestService(runner, &fakeSessions{sessions: map[domain.SessionID]domain.Session{"s1": sess}}, nil)
	svc.paths = paths
	if err := svc.record(context.Background(), job{session: "s1"}); err != nil {
		t.Fatal(err)
	}
	if paths.calls != 0 {
		t.Fatalf("changed paths calls after non-report job = %d, want 0", paths.calls)
	}
	if err := svc.record(context.Background(), job{session: "s1", report: "done: ok"}); err != nil {
		t.Fatal(err)
	}
	if paths.calls != 1 {
		t.Fatalf("changed paths calls after report = %d, want 1", paths.calls)
	}
}

type pathCallSpy struct{ calls int }

func (s *pathCallSpy) ChangedPaths(context.Context, domain.Session, string) ([]string, error) {
	s.calls++
	return nil, nil
}

type fixedChangedPaths struct {
	paths []string
	err   error
}

func (f fixedChangedPaths) ChangedPaths(context.Context, domain.Session, string) ([]string, error) {
	return f.paths, f.err
}

type fakePRFailureReader struct {
	evidence map[string]PRFailureEvidence
	calls    []string
}

func (f *fakePRFailureReader) ForPR(_ context.Context, url string) (PRFailureEvidence, error) {
	f.calls = append(f.calls, url)
	return f.evidence[url], nil
}

func TestFailureFactsAreDeterministicCappedAndClipped(t *testing.T) {
	sess := worker("s1", "ao")
	sess.PRs = []domain.PRFacts{{URL: "https://z/pr/2"}, {URL: "https://a/pr/1"}, {URL: "https://a/pr/1"}}
	long := strings.Repeat("tail ", 80)
	reader := &fakePRFailureReader{evidence: map[string]PRFailureEvidence{
		"https://a/pr/1": {
			Checks:   []FailedCheck{{Name: "z-check", LogTail: long}, {Name: "a-check", LogTail: long}, {Name: "b-check"}, {Name: "c-check"}},
			Comments: []UnresolvedComment{{Author: "zoe", Body: "z"}, {Author: "amy", File: "a.go", Body: strings.Repeat("comment ", 80)}, {Author: "ben", Body: "b"}, {Author: "cal", Body: "c"}},
		},
		"https://z/pr/2": {Checks: []FailedCheck{{Name: "earlier-by-url"}}, Comments: []UnresolvedComment{{Author: "aaron", Body: "earlier by url"}}},
	}}
	svc := newTestService(&fakeRunner{}, &fakeSessions{}, &fakeTasks{reports: map[domain.SessionID]taskreport.Report{"s1": {Outcome: taskreport.OutcomeFailed}}})
	svc.failures = reader
	fact := svc.factFor(context.Background(), sess)
	want := []string{"CI a-check: " + clip(long, 200), "CI b-check", "CI c-check", "Review by amy on a.go: " + clip(strings.Repeat("comment ", 80), 200), "Review by ben: b", "Review by cal: c"}
	if !reflect.DeepEqual(fact.Failures, want) {
		t.Fatalf("failures = %#v, want %#v", fact.Failures, want)
	}
	if !reflect.DeepEqual(reader.calls, []string{"https://a/pr/1", "https://z/pr/2"}) {
		t.Fatalf("PR reads = %#v, want sorted unique URLs", reader.calls)
	}
	for _, item := range fact.Failures {
		if utf8.RuneCountInString(item) > 320 {
			t.Fatalf("stored fact exceeds clipped fields: %d runes", utf8.RuneCountInString(item))
		}
	}
}

func TestFailureFactsSkipMergedAndInProgress(t *testing.T) {
	reader := &fakePRFailureReader{}
	svc := newTestService(&fakeRunner{}, &fakeSessions{}, &fakeTasks{reports: map[domain.SessionID]taskreport.Report{
		"merged": {Outcome: taskreport.OutcomeMerged},
		"live":   {Outcome: "in_progress"},
	}})
	svc.failures = reader
	for _, id := range []domain.SessionID{"merged", "live"} {
		sess := worker(id, "ao")
		sess.PRs = []domain.PRFacts{{URL: "https://x/pr/1"}}
		if got := svc.factFor(context.Background(), sess).Failures; len(got) != 0 {
			t.Fatalf("%s failures = %#v, want none", id, got)
		}
	}
	if len(reader.calls) != 0 {
		t.Fatalf("failure evidence queried for excluded outcomes: %#v", reader.calls)
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

// embedGraph scripts MissingEmbeddings batches, records SetEmbeddings and
// reports each statement kind on ran.
type embedGraph struct {
	mu      sync.Mutex
	batches [][]Row
	missErr error
	setErr  error
	misses  int
	sets    []map[string]any
	ran     chan string
}

func (g *embedGraph) Run(_ context.Context, statement string, params map[string]any) ([]Row, error) {
	kind, rows, err := "write", []Row(nil), error(nil)
	g.mu.Lock()
	switch {
	case strings.Contains(statement, "embedding IS NULL"):
		kind, err = "missing", g.missErr
		g.misses++
		if len(g.batches) > 0 {
			rows, g.batches = g.batches[0], g.batches[1:]
		}
	case strings.Contains(statement, "setNodeVectorProperty"):
		kind, err = "set", g.setErr
		g.sets = append(g.sets, params)
	}
	g.mu.Unlock()
	if g.ran != nil {
		g.ran <- kind
	}
	return rows, err
}

func (g *embedGraph) counts() (misses, sets int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.misses, len(g.sets)
}

func lessonRows(n int) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = Row{"label": "Lesson", "id": fmt.Sprintf("l%d", i), "text": fmt.Sprintf("text %d", i)}
	}
	return rows
}

func waitKind(t *testing.T, ran <-chan string, want string) {
	t.Helper()
	select {
	case got := <-ran:
		if got != want {
			t.Fatalf("statement kind = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %q", want)
	}
}

func TestEmbedMissingBatches(t *testing.T) {
	tests := []struct {
		name       string
		batches    [][]Row
		wantMisses int
		wantSets   int
		wantDocs   []int
	}{
		{name: "nothing missing", batches: nil, wantMisses: 1, wantSets: 0, wantDocs: []int{}},
		{name: "short batch stops", batches: [][]Row{lessonRows(3)}, wantMisses: 1, wantSets: 1, wantDocs: []int{3}},
		{name: "full batch then short", batches: [][]Row{lessonRows(embedBatchSize), lessonRows(2)}, wantMisses: 2, wantSets: 2, wantDocs: []int{embedBatchSize, 2}},
		{name: "full batch then empty", batches: [][]Row{lessonRows(embedBatchSize), {}}, wantMisses: 2, wantSets: 1, wantDocs: []int{embedBatchSize}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &embedGraph{batches: tt.batches}
			e := &fakeEmbedder{vec: []float64{1}}
			svc := newSemanticService(g, e, &syncBuffer{})

			err := svc.embedMissing(context.Background())

			if err != nil {
				t.Fatalf("embedMissing() error = %v", err)
			}
			if misses, sets := g.counts(); misses != tt.wantMisses || sets != tt.wantSets {
				t.Fatalf("misses=%d sets=%d, want %d/%d", misses, sets, tt.wantMisses, tt.wantSets)
			}
			docs := []int{}
			for _, d := range e.docCalls() {
				docs = append(docs, len(d))
			}
			if !reflect.DeepEqual(docs, tt.wantDocs) {
				t.Fatalf("embedded batch sizes = %v, want %v", docs, tt.wantDocs)
			}
		})
	}
}

func TestEmbedMissingStoresVectorsForTargets(t *testing.T) {
	g := &embedGraph{batches: [][]Row{{{"label": "Lesson", "id": "l1", "text": "run make sqlc"}}}}
	e := &fakeEmbedder{vec: []float64{0.5}}
	svc := newSemanticService(g, e, &syncBuffer{})

	if err := svc.embedMissing(context.Background()); err != nil {
		t.Fatalf("embedMissing() error = %v", err)
	}

	if got := e.docCalls(); !reflect.DeepEqual(got, [][]string{{"run make sqlc"}}) {
		t.Fatalf("embedded = %v", got)
	}
	want := []map[string]any{{"id": "l1", "v": []float64{0.5}}}
	if got := g.sets[0]["lessons"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("lessons = %#v, want %#v", got, want)
	}
}

func TestEmbedMissingReturnsErrors(t *testing.T) {
	tests := []struct {
		name    string
		graph   *embedGraph
		embed   *fakeEmbedder
		wantErr string
	}{
		{name: "missing query fails", graph: &embedGraph{missErr: errors.New("graph down")}, embed: &fakeEmbedder{}, wantErr: "graph down"},
		{name: "embedding fails", graph: &embedGraph{batches: [][]Row{lessonRows(1)}}, embed: &fakeEmbedder{docErr: errors.New("ollama down")}, wantErr: "ollama down"},
		{name: "store fails", graph: &embedGraph{batches: [][]Row{lessonRows(1)}, setErr: errors.New("bad dims")}, embed: &fakeEmbedder{vec: []float64{1}}, wantErr: "bad dims"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSemanticService(tt.graph, tt.embed, &syncBuffer{})

			err := svc.embedMissing(context.Background())

			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("embedMissing() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// runEmbedLoop runs embedLoop on tick and stops it when the test ends.
func runEmbedLoop(t *testing.T, svc *Service, tick <-chan time.Time) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		svc.embedLoop(ctx, tick)
		close(stopped)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(2 * time.Second):
			t.Errorf("embedLoop did not stop")
		}
	})
}

func TestEmbedLoopRunsOnTick(t *testing.T) {
	g := &embedGraph{ran: make(chan string, 4)}
	svc := newSemanticService(g, &fakeEmbedder{}, &syncBuffer{})
	tick := make(chan time.Time)
	runEmbedLoop(t, svc, tick)

	tick <- testAt

	waitKind(t, g.ran, "missing")
}

func TestEmbedLoopLogsFailures(t *testing.T) {
	logs := &syncBuffer{}
	g := &embedGraph{missErr: errors.New("graph down")}
	svc := newSemanticService(g, &fakeEmbedder{}, logs)
	runEmbedLoop(t, svc, nil)

	svc.kickEmbed()

	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), "embedding backfill failed") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := logs.String(); !strings.Contains(got, "embedding backfill failed") || !strings.Contains(got, "graph down") {
		t.Fatalf("logs = %q, want backfill failure", got)
	}
}

func TestStartWithEmbedderBackfillsOnStart(t *testing.T) {
	g := &embedGraph{ran: make(chan string, 4), batches: [][]Row{lessonRows(1)}}
	svc := newSemanticService(g, &fakeEmbedder{vec: []float64{1}}, &syncBuffer{})

	startRecorder(t, svc)

	waitKind(t, g.ran, "missing")
	waitKind(t, g.ran, "set")
}

func TestStartWithEmbedderKicksAfterRecord(t *testing.T) {
	g := &embedGraph{ran: make(chan string, 8)}
	svc := newSemanticService(g, &fakeEmbedder{}, &syncBuffer{})
	startRecorder(t, svc)
	waitKind(t, g.ran, "missing")

	svc.enqueue(job{session: "s1", report: "done: ok"})

	waitKind(t, g.ran, "write")
	waitKind(t, g.ran, "missing")
}

func TestRecordKicksEmbedAfterWrite(t *testing.T) {
	tests := []struct {
		name     string
		runErr   error
		wantKick int
	}{
		{name: "successful write kicks", wantKick: 1},
		{name: "failed write does not kick", runErr: errors.New("down"), wantKick: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newSemanticService(&fakeRunner{err: tt.runErr}, &fakeEmbedder{}, &syncBuffer{})

			_ = svc.record(context.Background(), job{session: "s1", report: "done: ok"})

			if len(svc.embedKick) != tt.wantKick {
				t.Fatalf("embedKick len = %d, want %d", len(svc.embedKick), tt.wantKick)
			}
		})
	}
}

func TestStartWithoutEmbedderRunsOnlyQueue(t *testing.T) {
	g := &embedGraph{ran: make(chan string, 4)}
	svc := newSemanticService(g, nil, &syncBuffer{})

	startRecorder(t, svc)
	kicked := len(svc.embedKick)
	svc.enqueue(job{session: "s1", report: "done: ok"})
	waitKind(t, g.ran, "write")

	if kicked != 0 {
		t.Fatalf("Start kicked the embed loop without an embedder")
	}
	if misses, _ := g.counts(); misses != 0 {
		t.Fatalf("misses = %d, want no embed loop", misses)
	}
}

func TestStartWithEmbedderStopsOnCancel(t *testing.T) {
	svc := newSemanticService(&embedGraph{}, &fakeEmbedder{}, &syncBuffer{})
	ctx, cancel := context.WithCancel(context.Background())
	done := svc.Start(ctx)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Start did not close its channel after cancel")
	}
}

func TestEmbedMissingProbesDimensionsOnce(t *testing.T) {
	g := &embedGraph{}
	e := &fakeEmbedder{vec: make([]float64, 3)}
	svc := newSemanticService(g, e, &syncBuffer{})

	for i := 0; i < 2; i++ {
		if err := svc.embedMissing(context.Background()); err != nil {
			t.Fatalf("embedMissing() error = %v", err)
		}
	}

	if svc.embedDims != 3 {
		t.Fatalf("embedDims = %d, want 3", svc.embedDims)
	}
	if n := len(e.queries); n != 1 {
		t.Fatalf("probe calls = %d, want 1 (cached)", n)
	}
	if misses, _ := g.counts(); misses != 2 {
		t.Fatalf("missing queries = %d, want 2", misses)
	}
}

func TestEmbedMissingReturnsProbeError(t *testing.T) {
	g := &embedGraph{}
	e := &fakeEmbedder{err: errors.New("ollama down")}
	svc := newSemanticService(g, e, &syncBuffer{})

	err := svc.embedMissing(context.Background())

	if err == nil || !strings.Contains(err.Error(), "ollama down") {
		t.Fatalf("embedMissing() error = %v, want probe error", err)
	}
	if misses, _ := g.counts(); misses != 0 {
		t.Fatalf("missing queries = %d, want 0 after a failed probe", misses)
	}
}

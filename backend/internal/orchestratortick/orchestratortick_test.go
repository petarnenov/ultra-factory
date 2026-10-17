package orchestratortick

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

type fakeDaemon struct {
	mu            sync.Mutex
	status        DaemonStatus
	statusErr     error
	orchestrators []Orchestrator
	sessions      map[string]SessionState
	workers       map[string][]Worker
	workerErr     map[string]error
	sendErr       error
	sent          []string
}

func (d *fakeDaemon) Status(context.Context) (DaemonStatus, error) { return d.status, d.statusErr }
func (d *fakeDaemon) Orchestrators(context.Context) ([]Orchestrator, error) {
	return d.orchestrators, nil
}
func (d *fakeDaemon) Session(_ context.Context, id string) (SessionState, error) {
	s, ok := d.sessions[id]
	if !ok {
		return SessionState{}, errors.New("not found")
	}
	return s, nil
}
func (d *fakeDaemon) Workers(_ context.Context, host string) ([]Worker, error) {
	return d.workers[host], d.workerErr[host]
}
func (d *fakeDaemon) Send(_ context.Context, id, msg string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sendErr != nil {
		return d.sendErr
	}
	d.sent = append(d.sent, id+"\n"+msg)
	return nil
}

type fakeGitHub struct {
	state GitHubState
	err   error
}

func (g fakeGitHub) State(context.Context, GitHubQuery) (GitHubState, error) {
	return g.state, g.err
}

// fixture mirrors the scenario of the former shell test: one idle
// orchestrator, one worker with a green, mergeable pull request.
func fixture(t *testing.T) (*Runner, *fakeDaemon, *fakeGitHub, FileStore) {
	t.Helper()
	d := &fakeDaemon{
		status:        DaemonStatus{Ready: true},
		orchestrators: []Orchestrator{{ID: "uf-orchestrator-7", ProjectID: "ultra-factory"}},
		sessions:      map[string]SessionState{"uf-orchestrator-7": {Status: "idle", Activity: "idle"}},
		workers: map[string][]Worker{"Linux": {
			{ID: "uf-41", Branch: "rw/RW-5.4.2", Activity: "idle", Harness: "claude-code", PRNumber: 201, CI: "passing", LastActivityAt: now.Add(-5 * time.Minute), CreatedAt: now.Add(-time.Hour)},
			{ID: "uf-9", Branch: "ao/uf-9/root", Activity: "idle"},
		}},
	}
	gh := &fakeGitHub{state: GitHubState{
		OpenPRs: []PullRequest{{Number: 201, Head: "rw/RW-5.4.2", Mergeable: "MERGEABLE", Checks: []Check{
			{Result: "SUCCESS", CompletedAt: now.Add(-40 * time.Minute)},
			{Result: "SKIPPED", CompletedAt: now.Add(-35 * time.Minute)},
		}}},
		OpenIssues: map[int]string{120: "RW-4.1"},
	}}
	store := FileStore{Dir: filepath.Join(t.TempDir(), "orchestrator-tick")}
	cfg := DefaultConfig()
	cfg.Hosts = []string{"Linux"}
	r := &Runner{Config: cfg, Daemon: d, GitHub: gh, Store: store, Now: func() time.Time { return now }}
	return r, d, gh, store
}

func tick(t *testing.T, r *Runner) Result {
	t.Helper()
	res, err := r.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestGreenPRWakesTheOrchestratorWithTheState(t *testing.T) {
	r, d, _, store := fixture(t)
	res := tick(t, r)
	if res.Outcome != OutcomeSent || res.Session != "uf-orchestrator-7" || res.Reason != "state changed" {
		t.Fatalf("result = %+v", res)
	}
	for _, want := range []string{
		"TICK (automatic wake-up from ao orchestrator tick",
		"[Linux]\n  uf-41 rw/RW-5.4.2 pr=#201 ci=passing idle\n",
		"  #201 rw/RW-5.4.2 mergeable=MERGEABLE checks=green\n",
		"VIOLATION: #201 (rw/RW-5.4.2) has been green and mergeable for 35 min",
		"FIRST fix these violations",
	} {
		if !strings.Contains(res.Message, want) {
			t.Errorf("message lacks %q:\n%s", want, res.Message)
		}
	}
	if strings.Contains(res.Message, "uf-9") {
		t.Errorf("message lists a non-backlog session:\n%s", res.Message)
	}
	if len(d.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(d.sent))
	}
	last, ok, err := store.LastTick()
	if err != nil || !ok || last.Outcome != OutcomeSent {
		t.Fatalf("last tick = %+v ok=%v err=%v", last, ok, err)
	}
}

func TestDryRunSendsNothingAndRecordsNothing(t *testing.T) {
	r, d, _, store := fixture(t)
	var out bytes.Buffer
	r.DryRun, r.DryRunOut = true, &out
	res := tick(t, r)
	if res.Outcome != OutcomeDryRun || !strings.Contains(out.String(), "TICK (") {
		t.Fatalf("result = %+v out=%q", res, out.String())
	}
	if len(d.sent) != 0 {
		t.Fatal("a dry run sent a message")
	}
	if _, ok, _ := store.LastSent(); ok {
		t.Fatal("a dry run recorded a send")
	}
	if _, ok, _ := store.LastTick(); ok {
		t.Fatal("a dry run recorded a tick")
	}
}

func TestASentStateIsNotResentUntilTheRenudgeInterval(t *testing.T) {
	r, d, _, _ := fixture(t)
	tick(t, r)
	if res := tick(t, r); res.Outcome != OutcomeUnchanged {
		t.Fatalf("second tick = %+v, want unchanged", res)
	}
	later := now.Add(r.Config.ViolationRenudge)
	r.Now = func() time.Time { return later }
	res := tick(t, r)
	if res.Outcome != OutcomeSent || !strings.HasPrefix(res.Reason, "invariant still violated after") {
		t.Fatalf("tick after the violation re-nudge interval = %+v", res)
	}
	if len(d.sent) != 2 {
		t.Fatalf("sent %d messages, want 2", len(d.sent))
	}
}

func TestRenudgeWithoutViolationsUsesTheLongerInterval(t *testing.T) {
	cfg := DefaultConfig()
	last := Sent{Fingerprint: "s", At: now}
	if got := decide(cfg, last, true, "s", nil, now.Add(cfg.ViolationRenudge)); got != "" {
		t.Fatalf("decide = %q before the re-nudge interval", got)
	}
	if got := decide(cfg, last, true, "s", nil, now.Add(cfg.Renudge)); !strings.HasPrefix(got, "no change for 30m") {
		t.Fatalf("decide = %q after the re-nudge interval", got)
	}
}

func TestNoOrchestratorOnlyLogs(t *testing.T) {
	for name, orchestrators := range map[string][]Orchestrator{
		"none": nil,
		"two":  {{ID: "uf-orchestrator-7", ProjectID: "ultra-factory"}, {ID: "uf-orchestrator-8", ProjectID: "ultra-factory"}},
	} {
		t.Run(name, func(t *testing.T) {
			r, d, _, _ := fixture(t)
			d.orchestrators = orchestrators
			res := tick(t, r)
			if res.Outcome != OutcomeNoOrchestrator || len(d.sent) != 0 {
				t.Fatalf("result = %+v sent=%d", res, len(d.sent))
			}
			if name == "two" && !strings.Contains(res.Reason, "2 live orchestrators in project ultra-factory (uf-orchestrator-7, uf-orchestrator-8)") {
				t.Fatalf("reason = %q", res.Reason)
			}
		})
	}
}

func TestPinnedSessionMustBeLive(t *testing.T) {
	r, d, _, _ := fixture(t)
	r.Config.Session = "uf-orchestrator-7"
	d.orchestrators = nil
	if res := tick(t, r); res.Outcome != OutcomeSent {
		t.Fatalf("pinned live session: %+v", res)
	}
	d.sessions["uf-orchestrator-7"] = SessionState{Terminated: true}
	if res := tick(t, r); res.Outcome != OutcomeNoOrchestrator || !strings.Contains(res.Reason, "is terminated") {
		t.Fatalf("pinned terminated session: %+v", res)
	}
}

func TestDaemonNotReadyOnlyLogs(t *testing.T) {
	r, d, _, _ := fixture(t)
	d.status = DaemonStatus{Detail: "state=starting health=degraded"}
	res := tick(t, r)
	if res.Outcome != OutcomeDaemonDown || !strings.Contains(res.Reason, "state=starting health=degraded") || len(d.sent) != 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestBusyOrchestratorIsNotInterrupted(t *testing.T) {
	r, d, _, _ := fixture(t)
	d.sessions["uf-orchestrator-7"] = SessionState{Status: "working", Activity: "active"}
	if res := tick(t, r); res.Outcome != OutcomeBusy || len(d.sent) != 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestUnavailableGitHubIsAViolation(t *testing.T) {
	r, d, gh, _ := fixture(t)
	gh.err = errors.New("github scm: auth failed")
	res := tick(t, r)
	if res.Outcome != OutcomeSent || len(res.Violations) == 0 || !strings.Contains(res.Violations[0].Text, "the GitHub state is unavailable (github scm: auth failed)") {
		t.Fatalf("result = %+v", res)
	}
	if strings.Contains(d.sent[0], "mergeable=") {
		t.Fatal("pull requests listed without GitHub data")
	}
}

func TestUnreachableHostIsReported(t *testing.T) {
	r, d, _, _ := fixture(t)
	d.workerErr = map[string]error{"Linux": errors.New("context deadline exceeded")}
	res := tick(t, r)
	if !strings.Contains(res.Message, "[Linux]\n  unreachable: context deadline exceeded\n") {
		t.Fatalf("message:\n%s", res.Message)
	}
}

func TestPauseFlagAndEnvSkipTheTick(t *testing.T) {
	r, d, _, store := fixture(t)
	if err := store.Pause("redeploy by ops"); err != nil {
		t.Fatal(err)
	}
	if res := tick(t, r); res.Outcome != OutcomePaused || res.Reason != "redeploy by ops" || len(d.sent) != 0 {
		t.Fatalf("flag: %+v", res)
	}
	if was, err := store.Resume(); err != nil || !was {
		t.Fatalf("resume = %v, %v", was, err)
	}
	r.Config.Paused = true
	if res := tick(t, r); res.Outcome != OutcomePaused || res.Reason != "RW_TICK_PAUSED=1" {
		t.Fatalf("env: %+v", res)
	}
}

func TestNothingInFlightSendsNothing(t *testing.T) {
	r, d, gh, _ := fixture(t)
	d.workers = nil
	gh.state = GitHubState{}
	if res := tick(t, r); res.Outcome != OutcomeNothing || len(d.sent) != 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestFailedSendIsNotRecordedAsSent(t *testing.T) {
	r, d, _, store := fixture(t)
	d.sendErr = errors.New("connection refused")
	if res := tick(t, r); res.Outcome != OutcomeSendFailed {
		t.Fatalf("result = %+v", res)
	}
	if _, ok, _ := store.LastSent(); ok {
		t.Fatal("a failed send was recorded as sent")
	}
}

func TestLoopTicksRepeatedlyAndStopsOnCancel(t *testing.T) {
	r, _, _, store := fixture(t)
	r.Config.Paused = true
	r.Config.Interval = 10 * time.Millisecond
	var logs bytes.Buffer
	r.Log = slog.New(slog.NewTextHandler(&syncWriter{w: &logs}, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Loop(ctx) }()
	deadline := time.After(5 * time.Second)
	for strings.Count(readSync(&logs), "outcome=paused") < 3 {
		select {
		case <-deadline:
			t.Fatalf("loop did not tick three times:\n%s", readSync(&logs))
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not stop on cancel")
	}
	if !strings.Contains(readSync(&logs), "tick loop stopped") {
		t.Fatalf("no stop line:\n%s", readSync(&logs))
	}
	if rec, ok, _ := store.LastTick(); !ok || rec.Outcome != OutcomePaused {
		t.Fatalf("last tick = %+v", rec)
	}
}

func TestLoopSurvivesAPanickingTick(t *testing.T) {
	r, _, _, _ := fixture(t)
	r.Daemon = &panicDaemon{}
	r.Config.Interval = 10 * time.Millisecond
	var logs bytes.Buffer
	r.Log = slog.New(slog.NewTextHandler(&syncWriter{w: &logs}, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Loop(ctx) }()
	deadline := time.After(5 * time.Second)
	for strings.Count(readSync(&logs), "tick panicked") < 2 {
		select {
		case <-deadline:
			t.Fatalf("loop did not survive panics:\n%s", readSync(&logs))
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done
}

type panicDaemon struct{ fakeDaemon }

func (*panicDaemon) Status(context.Context) (DaemonStatus, error) { panic("boom") }

var logMu sync.Mutex

type syncWriter struct{ w *bytes.Buffer }

func (s *syncWriter) Write(p []byte) (int, error) {
	logMu.Lock()
	defer logMu.Unlock()
	return s.w.Write(p)
}

func readSync(b *bytes.Buffer) string {
	logMu.Lock()
	defer logMu.Unlock()
	return b.String()
}

func TestStoreFilesArePrivate(t *testing.T) {
	_, _, _, store := fixture(t)
	if err := store.SaveSent(Sent{Session: "s", Fingerprint: "x", At: now}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("state dir mode = %o, want 700", perm)
	}
	got, ok, err := store.LastSent()
	if err != nil || !ok || got.Fingerprint != "x" || !got.At.Equal(now) {
		t.Fatalf("LastSent = %+v ok=%v err=%v", got, ok, err)
	}
	entries, _ := os.ReadDir(store.Dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temporary file left behind: %s", e.Name())
		}
	}
}

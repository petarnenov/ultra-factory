package session

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// harnessReadiness answers readiness by harness; a missing harness is ready.
type harnessReadiness map[domain.AgentHarness]domain.AgentEffectiveReadiness

func (h harnessReadiness) EnsureAgentReadiness(_ context.Context, id string, _ domain.AgentReadinessPurpose) (domain.AgentReadinessSnapshot, error) {
	state, ok := h[domain.AgentHarness(id)]
	if !ok {
		state = domain.AgentReadinessReady
	}
	snap := domain.AgentReadinessSnapshot{ID: id, EffectiveReadiness: state}
	if state == domain.AgentReadinessNotReady {
		snap.Installation.State = domain.AgentInstallationNotInstalled
	}
	return snap, nil
}
func (harnessReadiness) InvalidateAgentInstallation(string)   {}
func (harnessReadiness) InvalidateAgentAuthentication(string) {}
func (harnessReadiness) RecheckAgent(string)                  {}

var (
	profileClaude = domain.AgentProfile{ID: "a", Harness: domain.HarnessClaudeCode, Model: "sonnet", Effort: "medium", Weight: 3}
	profileCodex  = domain.AgentProfile{ID: "b", Harness: domain.HarnessCodex, Model: "gpt-5", Effort: "high"}
)

func fixedSel(id string) domain.AgentSelection {
	return domain.AgentSelection{Mode: domain.AgentSelectionFixed, ProfileID: id}
}

func randomSel(ids ...string) domain.AgentSelection {
	return domain.AgentSelection{Mode: domain.AgentSelectionRandom, ProfileIDs: ids}
}

func (f *capFixture) withProfiles(cfg domain.CoordinatorsConfig) {
	cfg.Enabled = true
	cfg.AgentProfiles = []domain.AgentProfile{profileClaude, profileCodex}
	f.configure(cfg)
}

// finishLast terminates the newest spawned session so later spawns are not
// bound by the capacity it used.
func (f *capFixture) finishLast() {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := domain.SessionID(fmt.Sprintf("ao-n%d", f.next))
	rec := f.store.sessions[id]
	rec.IsTerminated = true
	f.store.sessions[id] = rec
}

func (f *capFixture) lastCfg() ports.SpawnConfig { return f.cfgs[len(f.cfgs)-1] }

func (f *capFixture) spawnCoordinatorWith(cfg ports.SpawnConfig) (domain.SessionID, error) {
	cfg.ProjectID, cfg.Kind, cfg.ParentSessionID, cfg.CoordinatorScope = "ao", domain.KindCoordinator, "ao-root", "scope"
	sess, _, _, err := f.svc.Spawn(context.Background(), cfg)
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func TestFixedCoordinatorSpawnsOnlyItsProfile(t *testing.T) {
	f := newCapFixture(t)
	f.withProfiles(domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 6, MaxWorkers: 12})
	sel := fixedSel("a")
	id, err := f.spawnCoordinatorWith(ports.SpawnConfig{CoordinatorWorkerAgent: &sel})
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if err := f.spawnWorker(id); err != nil {
			t.Fatal(err)
		}
		cfg := f.lastCfg()
		if cfg.Harness != domain.HarnessClaudeCode || cfg.AgentConfig.Model != "sonnet" || cfg.AgentConfig.Effort != "medium" ||
			cfg.AgentProfileID != "a" || !cfg.HarnessExplicit || !cfg.EffortOverride {
			t.Fatalf("worker spawn = %+v", cfg)
		}
	}
}

func TestRandomCoordinatorIsWeightedOver200SeededSpawns(t *testing.T) {
	f := newCapFixture(t)
	f.withProfiles(domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 6, MaxWorkers: 12})
	rng := rand.New(rand.NewPCG(7, 9))
	f.svc.profileIntn = rng.IntN
	sel := randomSel("a", "b")
	id, err := f.spawnCoordinatorWith(ports.SpawnConfig{CoordinatorWorkerAgent: &sel})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for range 200 {
		if err := f.spawnWorker(id); err != nil {
			t.Fatal(err)
		}
		counts[f.lastCfg().AgentProfileID]++
		f.finishLast()
	}
	if counts["a"] < 130 || counts["a"] > 170 || counts["a"]+counts["b"] != 200 {
		t.Fatalf("weights 3:1 not respected: %v", counts)
	}
}

func TestUnauthorizedProfileIsNeverChosen(t *testing.T) {
	f := newCapFixture(t)
	f.withProfiles(domain.CoordinatorsConfig{})
	f.svc.agentReadiness = harnessReadiness{domain.HarnessClaudeCode: domain.AgentReadinessNotReady}
	f.svc.profileIntn = rand.New(rand.NewPCG(1, 1)).IntN
	sel := randomSel("a", "b")
	id, err := f.spawnCoordinatorWith(ports.SpawnConfig{CoordinatorWorkerAgent: &sel})
	if err != nil {
		t.Fatal(err)
	}
	for range 30 {
		if err := f.spawnWorker(id); err != nil {
			t.Fatal(err)
		}
		if got := f.lastCfg().AgentProfileID; got != "b" {
			t.Fatalf("picked %q while a is not ready", got)
		}
		f.finishLast()
	}
}

func TestOnlyUnreadyProfileRefusesTheSpawnWithoutSideEffects(t *testing.T) {
	f := newCapFixture(t)
	f.withProfiles(domain.CoordinatorsConfig{})
	sel := fixedSel("a")
	id, err := f.spawnCoordinatorWith(ports.SpawnConfig{CoordinatorWorkerAgent: &sel})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.agentReadiness = harnessReadiness{domain.HarnessClaudeCode: domain.AgentReadinessNotReady}
	before := len(f.cfgs)
	wantAPIError(t, f.spawnWorker(id), "WORKER_PROFILE_UNAVAILABLE", map[string]any{"coordinatorId": string(id)})
	if len(f.cfgs) != before {
		t.Fatal("a refused spawn must not reach the manager")
	}
}

func TestExplicitHarnessOutsideTheSelectionIsNotAllowed(t *testing.T) {
	f := newCapFixture(t)
	f.withProfiles(domain.CoordinatorsConfig{})
	sel := randomSel("a", "b")
	id, err := f.spawnCoordinatorWith(ports.SpawnConfig{CoordinatorWorkerAgent: &sel})
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.cfgs)
	_, _, _, err = f.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindWorker, ParentSessionID: id, Harness: "aider", HarnessExplicit: true, HarnessExplicitSet: true,
	})
	wantAPIError(t, err, "WORKER_PROFILE_NOT_ALLOWED", map[string]any{"coordinatorId": string(id)})
	if len(f.cfgs) != before {
		t.Fatal("a refused spawn must not reach the manager")
	}
}

func TestInheritAndNoProfilesKeepTheSpawnUntouched(t *testing.T) {
	f := newCapFixture(t)
	id := f.coordinator("ao-c1", false)
	if err := f.spawnWorker(id); err != nil {
		t.Fatal(err)
	}
	cfg := f.lastCfg()
	if cfg.AgentProfileID != "" || cfg.Harness != "" || cfg.EffortOverride {
		t.Fatalf("spawn changed without profiles: %+v", cfg)
	}
	f.withProfiles(domain.CoordinatorsConfig{})
	if err := f.spawnWorker(id); err != nil {
		t.Fatal(err)
	}
	if cfg := f.lastCfg(); cfg.AgentProfileID != "" || cfg.Harness != "" {
		t.Fatalf("inherit must not pick a profile: %+v", cfg)
	}
}

func TestCoordinatorWithoutOwnSelectionUsesTheProjectWorkerAgent(t *testing.T) {
	f := newCapFixture(t)
	f.withProfiles(domain.CoordinatorsConfig{WorkerAgent: fixedSel("b")})
	id := f.coordinator("ao-legacy", false)
	if err := f.spawnWorker(id); err != nil {
		t.Fatal(err)
	}
	if got := f.lastCfg().AgentProfileID; got != "b" {
		t.Fatalf("profile = %q, want the project default b", got)
	}
}

func TestRootWorkerAgentAppliesToRootSpawnsAndFlagsWin(t *testing.T) {
	f := newCapFixture(t)
	f.withProfiles(domain.CoordinatorsConfig{RootWorkerAgent: fixedSel("b")})
	if err := f.spawnWorker("ao-root"); err != nil {
		t.Fatal(err)
	}
	if cfg := f.lastCfg(); cfg.AgentProfileID != "b" || cfg.Harness != domain.HarnessCodex {
		t.Fatalf("root worker = %+v", cfg)
	}
	_, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindWorker, ParentSessionID: "ao-root", Harness: "aider", HarnessExplicit: true, HarnessExplicitSet: true,
	})
	if err != nil {
		t.Fatalf("explicit flags win for a root-owned worker: %v", err)
	}
	if cfg := f.lastCfg(); cfg.Harness != "aider" || cfg.AgentProfileID != "" {
		t.Fatalf("root worker with flags = %+v", cfg)
	}
	f.svc.agentReadiness = harnessReadiness{domain.HarnessCodex: domain.AgentReadinessNotReady}
	err = f.spawnWorker("ao-root")
	wantAPIError(t, err, "WORKER_PROFILE_UNAVAILABLE", nil)
}

func TestCoordinatorAgentPicksTheCoordinatorsOwnProfile(t *testing.T) {
	f := newCapFixture(t)
	f.withProfiles(domain.CoordinatorsConfig{CoordinatorAgent: fixedSel("a")})
	if _, err := f.spawnCoordinatorWith(ports.SpawnConfig{}); err != nil {
		t.Fatal(err)
	}
	if cfg := f.lastCfg(); cfg.Harness != domain.HarnessClaudeCode || cfg.AgentProfileID != "a" {
		t.Fatalf("coordinator = %+v", cfg)
	}
	// A per-coordinator override beats the project default.
	override := fixedSel("b")
	id, err := f.spawnCoordinatorWith(ports.SpawnConfig{CoordinatorAgent: &override})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.store.sessions[id]; got.AgentProfileID != "b" || got.Harness != domain.HarnessCodex {
		t.Fatalf("stored coordinator = %+v", got)
	}
}

func TestCoordinatorAgentRefusals(t *testing.T) {
	f := newCapFixture(t)
	f.withProfiles(domain.CoordinatorsConfig{CoordinatorAgent: fixedSel("a")})
	unknown := fixedSel("nope")
	_, err := f.spawnCoordinatorWith(ports.SpawnConfig{CoordinatorAgent: &unknown})
	wantAPIError(t, err, "COORDINATOR_PROFILE_NOT_ALLOWED", map[string]any{"profileId": "nope"})
	_, err = f.spawnCoordinatorWith(ports.SpawnConfig{Harness: "aider", HarnessExplicit: true, HarnessExplicitSet: true})
	wantAPIError(t, err, "COORDINATOR_PROFILE_NOT_ALLOWED", nil)
	badWorker := fixedSel("nope")
	_, err = f.spawnCoordinatorWith(ports.SpawnConfig{CoordinatorWorkerAgent: &badWorker})
	wantAPIError(t, err, "WORKER_PROFILE_INVALID", map[string]any{"field": "workerAgent"})
	f.svc.agentReadiness = harnessReadiness{domain.HarnessClaudeCode: domain.AgentReadinessNotReady}
	_, err = f.spawnCoordinatorWith(ports.SpawnConfig{})
	wantAPIError(t, err, "WORKER_PROFILE_UNAVAILABLE", map[string]any{"scope": "coordinator"})
	if len(f.cfgs) != 0 {
		t.Fatal("refused coordinator spawns must not reach the manager")
	}
}

func TestAgentSelectionsAreCoordinatorSpawnOnly(t *testing.T) {
	f := newCapFixture(t)
	sel := fixedSel("a")
	_, _, _, err := f.svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "ao", Kind: domain.KindWorker, ParentSessionID: "ao-root", CoordinatorWorkerAgent: &sel,
	})
	wantAPIError(t, err, "WORKER_PROFILE_INVALID", nil)
}

func TestLiveWorkerAgentChangeAppliesToLaterSpawnsOnly(t *testing.T) {
	f := newCapFixture(t)
	f.withProfiles(domain.CoordinatorsConfig{})
	first := fixedSel("a")
	id, err := f.spawnCoordinatorWith(ports.SpawnConfig{CoordinatorWorkerAgent: &first})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.spawnWorker(id); err != nil {
		t.Fatal(err)
	}
	if f.lastCfg().AgentProfileID != "a" {
		t.Fatal("first worker should run on a")
	}
	if _, err := f.svc.SetCoordinatorWorkerAgent(context.Background(), id, fixedSel("b"), opCaller("cli")); err != nil {
		t.Fatal(err)
	}
	if err := f.spawnWorker(id); err != nil {
		t.Fatal(err)
	}
	if f.lastCfg().AgentProfileID != "b" {
		t.Fatal("a worker spawned after the change should run on b")
	}
	if len(f.store.agentEvents) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(f.store.agentEvents))
	}
	ev := f.store.agentEvents[0]
	if ev.Origin != "cli" || ev.ActorSessionID != "ao-root" || ev.PreviousSelection == nil || ev.PreviousSelection.ProfileID != "a" || ev.NewSelection.ProfileID != "b" {
		t.Fatalf("audit = %+v", ev)
	}
	if len(f.fc.sentMessages) != 1 {
		t.Fatalf("system messages = %d, want 1", len(f.fc.sentMessages))
	}
	// The same selection again is a no-op: no row, no message.
	if _, err := f.svc.SetCoordinatorWorkerAgent(context.Background(), id, fixedSel("b"), rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	if len(f.store.agentEvents) != 1 || len(f.fc.sentMessages) != 1 {
		t.Fatal("an unchanged selection must write nothing")
	}
}

func TestLiveWorkerAgentChangeRules(t *testing.T) {
	f := newCapFixture(t)
	f.withProfiles(domain.CoordinatorsConfig{})
	id := f.coordinator("ao-c1", false)
	ctx := context.Background()
	_, err := f.svc.SetCoordinatorWorkerAgent(ctx, id, fixedSel("nope"), opCaller("ui"))
	wantAPIError(t, err, "WORKER_PROFILE_INVALID", map[string]any{"field": "workerAgent"})
	_, err = f.svc.SetCoordinatorWorkerAgent(ctx, id, fixedSel("a"), rootCaller("ao-c1"))
	wantAPIError(t, err, "COORDINATOR_ROOT_REQUIRED", nil)
	_, err = f.svc.SetCoordinatorWorkerAgent(ctx, id, fixedSel("a"), CoordinatorAgentCaller{OperatorInitiated: true, RequestedBy: "ao-root", Origin: "cli"})
	wantAPIError(t, err, "OPERATOR_MARKER_NOT_ALLOWED", nil)
	dead := f.coordinator("ao-dead", true)
	_, err = f.svc.SetCoordinatorWorkerAgent(ctx, dead, fixedSel("a"), opCaller("cli"))
	wantAPIError(t, err, "OWNER_TERMINATED", nil)
	if len(f.store.agentEvents) != 0 {
		t.Fatal("refused changes must not be audited")
	}
	// An inherit selection is a real change from the project default.
	if _, err := f.svc.SetCoordinatorWorkerAgent(ctx, id, domain.AgentSelection{}, opCaller("api")); err != nil {
		t.Fatal(err)
	}
	if got := f.store.sessions[id].CoordinatorWorkerAgent; got == nil || got.Mode != domain.AgentSelectionInherit {
		t.Fatalf("stored = %+v", got)
	}
}

package agentprofile

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

var (
	profileA = domain.AgentProfile{ID: "a", Harness: domain.HarnessClaudeCode, Model: "sonnet", Effort: "medium", Weight: 3}
	profileB = domain.AgentProfile{ID: "b", Harness: domain.HarnessCodex, Model: "gpt-5", Effort: "high"}
	profiles = []domain.AgentProfile{profileA, profileB}
)

func allReady(context.Context, domain.AgentHarness) (bool, string) { return true, "" }

func onlyReady(ready ...domain.AgentHarness) ReadyFunc {
	return func(_ context.Context, h domain.AgentHarness) (bool, string) {
		for _, r := range ready {
			if r == h {
				return true, ""
			}
		}
		return false, "not_installed"
	}
}

func apiCode(t *testing.T, err error) string {
	t.Helper()
	var api *apierr.Error
	if !errors.As(err, &api) {
		t.Fatalf("want an API error, got %v", err)
	}
	return api.Code
}

func TestFixedPicksOnlyItsProfile(t *testing.T) {
	for range 20 {
		res, err := Resolve(context.Background(), Request{
			Scope: ScopeCoordinatorWorker, ProjectID: "p", Coordinator: "c-1", Profiles: profiles,
			Selection: domain.AgentSelection{Mode: domain.AgentSelectionFixed, ProfileID: "a"}, Ready: allReady,
		})
		if err != nil || res.Profile == nil || res.Profile.ID != "a" || res.RecordID != "a" {
			t.Fatalf("got %+v, %v", res, err)
		}
	}
}

func TestRandomIsWeightedAndDeterministicWithASeed(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	counts := map[string]int{}
	for range 200 {
		res, err := Resolve(context.Background(), Request{
			Scope: ScopeCoordinatorWorker, ProjectID: "p", Profiles: profiles,
			Selection: domain.AgentSelection{Mode: domain.AgentSelectionRandom, ProfileIDs: []string{"a", "b"}},
			Ready:     allReady, Intn: rng.IntN,
		})
		if err != nil {
			t.Fatal(err)
		}
		counts[res.Profile.ID]++
	}
	// Weights 3:1 over 200 picks: expect about 150 and 50.
	if counts["a"] < 130 || counts["a"] > 170 || counts["a"]+counts["b"] != 200 {
		t.Fatalf("weights not respected: %v", counts)
	}
	again := rand.New(rand.NewPCG(1, 2))
	rerun := map[string]int{}
	for range 200 {
		res, _ := Resolve(context.Background(), Request{
			Scope: ScopeCoordinatorWorker, ProjectID: "p", Profiles: profiles,
			Selection: domain.AgentSelection{Mode: domain.AgentSelectionRandom},
			Ready:     allReady, Intn: again.IntN,
		})
		rerun[res.Profile.ID]++
	}
	if rerun["a"] != counts["a"] {
		t.Fatalf("a seeded source must repeat: %v vs %v", rerun, counts)
	}
}

func TestRandomSkipsUnreadyProfiles(t *testing.T) {
	for range 30 {
		res, err := Resolve(context.Background(), Request{
			Scope: ScopeCoordinatorWorker, ProjectID: "p", Profiles: profiles,
			Selection: domain.AgentSelection{Mode: domain.AgentSelectionRandom},
			Ready:     onlyReady(domain.HarnessCodex),
		})
		if err != nil || res.Profile.ID != "b" {
			t.Fatalf("got %+v, %v", res, err)
		}
	}
}

func TestUnavailableListsExcludedProfiles(t *testing.T) {
	_, err := Resolve(context.Background(), Request{
		Scope: ScopeCoordinatorWorker, ProjectID: "p", HostID: "h1", Coordinator: "c-1", Profiles: profiles,
		Selection: domain.AgentSelection{Mode: domain.AgentSelectionFixed, ProfileID: "a"},
		Ready:     onlyReady(domain.HarnessCodex),
	})
	var api *apierr.Error
	if !errors.As(err, &api) || api.Code != "WORKER_PROFILE_UNAVAILABLE" {
		t.Fatalf("got %v", err)
	}
	if api.Details["coordinatorId"] != "c-1" || api.Details["hostId"] != "h1" || len(api.Details["profiles"].([]map[string]any)) != 1 {
		t.Fatalf("details: %v", api.Details)
	}
}

func TestRootWorkerUnavailableOmitsCoordinatorID(t *testing.T) {
	_, err := Resolve(context.Background(), Request{
		Scope: ScopeRootWorker, ProjectID: "p", Coordinator: "c-1", Profiles: profiles,
		Selection: domain.AgentSelection{Mode: domain.AgentSelectionFixed, ProfileID: "a"},
		Ready:     onlyReady(),
	})
	var api *apierr.Error
	if !errors.As(err, &api) || api.Code != "WORKER_PROFILE_UNAVAILABLE" {
		t.Fatalf("got %v", err)
	}
	if _, ok := api.Details["coordinatorId"]; ok {
		t.Fatalf("root-owned details must omit coordinatorId: %v", api.Details)
	}
}

func TestCoordinatorWorkerExplicitFlagsMustMatchSelection(t *testing.T) {
	sel := domain.AgentSelection{Mode: domain.AgentSelectionRandom, ProfileIDs: []string{"a", "b"}}
	_, err := Resolve(context.Background(), Request{
		Scope: ScopeCoordinatorWorker, ProjectID: "p", Profiles: profiles, Selection: sel, Ready: allReady,
		Explicit: Explicit{Harness: "aider"},
	})
	if got := apiCode(t, err); got != "WORKER_PROFILE_NOT_ALLOWED" {
		t.Fatalf("code = %s", got)
	}
	res, err := Resolve(context.Background(), Request{
		Scope: ScopeCoordinatorWorker, ProjectID: "p", Profiles: profiles, Selection: sel, Ready: allReady,
		Explicit: Explicit{Harness: domain.HarnessCodex},
	})
	if err != nil || res.Profile.ID != "b" {
		t.Fatalf("a harness flag narrows to its profile: %+v, %v", res, err)
	}
}

func TestExplicitFlagOutsideTheSelectionIsRefused(t *testing.T) {
	_, err := Resolve(context.Background(), Request{
		Scope: ScopeCoordinatorWorker, ProjectID: "p", Profiles: profiles, Ready: allReady,
		Selection: domain.AgentSelection{Mode: domain.AgentSelectionFixed, ProfileID: "a"},
		Explicit:  Explicit{Harness: domain.HarnessCodex, Model: "gpt-5", Effort: "high"},
	})
	if got := apiCode(t, err); got != "WORKER_PROFILE_NOT_ALLOWED" {
		t.Fatalf("code = %s", got)
	}
}

func TestRootWorkerExplicitFlagsAlwaysWin(t *testing.T) {
	res, err := Resolve(context.Background(), Request{
		Scope: ScopeRootWorker, ProjectID: "p", Profiles: profiles, Ready: onlyReady(),
		Selection: domain.AgentSelection{Mode: domain.AgentSelectionFixed, ProfileID: "a"},
		Explicit:  Explicit{Harness: "aider"},
	})
	if err != nil || res.Profile != nil || res.RecordID != "" {
		t.Fatalf("got %+v, %v", res, err)
	}
	res, err = Resolve(context.Background(), Request{
		Scope: ScopeRootWorker, ProjectID: "p", Profiles: profiles, Ready: onlyReady(),
		Selection: domain.AgentSelection{Mode: domain.AgentSelectionFixed, ProfileID: "a"},
		Explicit:  Explicit{Harness: domain.HarnessCodex, Model: "gpt-5", Effort: "high"},
	})
	if err != nil || res.Profile != nil || res.RecordID != "b" {
		t.Fatalf("flags matching one profile record its ID: %+v, %v", res, err)
	}
}

func TestInheritKeepsFlagsAndRecordsAnExactMatch(t *testing.T) {
	res, err := Resolve(context.Background(), Request{
		Scope: ScopeCoordinatorWorker, ProjectID: "p", Profiles: profiles, Ready: onlyReady(),
		Explicit: Explicit{Harness: "aider", Model: "x"},
	})
	if err != nil || res.Profile != nil || res.RecordID != "" {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestCoordinatorScopeCodes(t *testing.T) {
	sel := domain.AgentSelection{Mode: domain.AgentSelectionFixed, ProfileID: "a"}
	_, err := Resolve(context.Background(), Request{
		Scope: ScopeCoordinator, ProjectID: "p", Profiles: profiles, Selection: sel, Ready: allReady,
		Explicit: Explicit{Harness: domain.HarnessCodex},
	})
	if got := apiCode(t, err); got != "COORDINATOR_PROFILE_NOT_ALLOWED" {
		t.Fatalf("code = %s", got)
	}
	_, err = Resolve(context.Background(), Request{
		Scope: ScopeCoordinator, ProjectID: "p", Profiles: profiles, Selection: sel, Ready: onlyReady(),
	})
	var api *apierr.Error
	if !errors.As(err, &api) || api.Code != "WORKER_PROFILE_UNAVAILABLE" || api.Details["scope"] != "coordinator" {
		t.Fatalf("got %v", err)
	}
}

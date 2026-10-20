package domain

import (
	"errors"
	"testing"
)

func TestCoordinatorsConfigDefaultsToDisabledWithTheADRLimits(t *testing.T) {
	var cfg CoordinatorsConfig
	if cfg.Enabled {
		t.Fatal("coordinators must be disabled by default")
	}
	if got := cfg.Limits(); got != (CoordinatorLimits{Coordinators: 2, WorkersPerCoordinator: 3, Workers: 6}) {
		t.Fatalf("default limits = %+v", got)
	}
	cfg = CoordinatorsConfig{MaxCoordinators: 1, MaxWorkers: 4}
	if got := cfg.Limits(); got != (CoordinatorLimits{Coordinators: 1, WorkersPerCoordinator: 3, Workers: 4}) {
		t.Fatalf("lowered limits = %+v", got)
	}
}

func TestCoordinatorsConfigRejectsLimitsOutsideTheHardMaximum(t *testing.T) {
	for field, tc := range map[string]struct {
		cfg CoordinatorsConfig
		max int
	}{
		"maxCoordinators":          {CoordinatorsConfig{MaxCoordinators: 5}, 4},
		"maxWorkersPerCoordinator": {CoordinatorsConfig{MaxWorkersPerCoordinator: 7}, 6},
		"maxWorkers":               {CoordinatorsConfig{MaxWorkers: 13}, 12},
	} {
		var limitsErr *CoordinatorLimitsError
		if err := (ProjectConfig{Coordinators: tc.cfg}).Validate(); !errors.As(err, &limitsErr) || limitsErr.Field != field || limitsErr.Max != tc.max {
			t.Fatalf("%s: err = %v", field, err)
		}
	}
	for field, cfg := range map[string]CoordinatorsConfig{
		"maxCoordinators":          {MaxCoordinators: -1},
		"maxWorkersPerCoordinator": {MaxWorkersPerCoordinator: -1},
		"maxWorkers":               {MaxWorkers: -1},
	} {
		var limitsErr *CoordinatorLimitsError
		if err := (ProjectConfig{Coordinators: cfg}).Validate(); !errors.As(err, &limitsErr) || limitsErr.Field != field {
			t.Fatalf("%s: negative value err = %v", field, err)
		}
	}
}

func TestCoordinatorsConfigAcceptsValuesUpToTheHardMaximum(t *testing.T) {
	for _, cfg := range []CoordinatorsConfig{
		{},
		{Enabled: true, MaxCoordinators: 2, MaxWorkersPerCoordinator: 3, MaxWorkers: 6},
		{Enabled: true, MaxCoordinators: 4, MaxWorkersPerCoordinator: 6, MaxWorkers: 12},
	} {
		if err := (ProjectConfig{Coordinators: cfg}).Validate(); err != nil {
			t.Fatalf("%+v is valid: %v", cfg, err)
		}
	}
	raised := CoordinatorsConfig{MaxCoordinators: 4, MaxWorkersPerCoordinator: 6, MaxWorkers: 12}
	if got := raised.Limits(); got != HardCoordinatorLimits {
		t.Fatalf("limits at the hard maximum = %+v", got)
	}
	if DefaultCoordinatorLimits != (CoordinatorLimits{Coordinators: 2, WorkersPerCoordinator: 3, Workers: 6}) {
		t.Fatalf("the defaults must stay 2/3/6, got %+v", DefaultCoordinatorLimits)
	}
}

func TestCoordinatorsConfigRejectsWorkersPerCoordinatorAboveTheProjectTotal(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg CoordinatorsConfig
		max int
	}{
		"both set":            {CoordinatorsConfig{MaxWorkersPerCoordinator: 5, MaxWorkers: 4}, 4},
		"total only, default": {CoordinatorsConfig{MaxWorkers: 2}, 2},
	} {
		var limitsErr *CoordinatorLimitsError
		err := (ProjectConfig{Coordinators: tc.cfg}).Validate()
		if !errors.As(err, &limitsErr) || limitsErr.Field != "maxWorkersPerCoordinator" || limitsErr.Max != tc.max {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	for name, cfg := range map[string]CoordinatorsConfig{
		"per-coordinator vs default total": {MaxWorkersPerCoordinator: 5},
		"total vs default per-coordinator": {MaxWorkers: 4},
		"equal":                            {MaxWorkersPerCoordinator: 4, MaxWorkers: 4},
	} {
		if err := (ProjectConfig{Coordinators: cfg}).Validate(); err != nil {
			t.Fatalf("%s: %+v is valid: %v", name, cfg, err)
		}
	}
}

func TestAgentProfilesValidate(t *testing.T) {
	good := AgentProfile{ID: "a", Harness: HarnessClaudeCode, Model: "sonnet", Effort: "medium"}
	for name, tc := range map[string]struct {
		cfg   CoordinatorsConfig
		field string
	}{
		"empty id":           {CoordinatorsConfig{AgentProfiles: []AgentProfile{{Harness: HarnessCodex, Model: "m", Effort: "e"}}}, "coordinators.agentProfiles[0].id"},
		"duplicate id":       {CoordinatorsConfig{AgentProfiles: []AgentProfile{good, good}}, "coordinators.agentProfiles[1].id"},
		"unknown harness":    {CoordinatorsConfig{AgentProfiles: []AgentProfile{{ID: "x", Harness: "nope", Model: "m", Effort: "e"}}}, "coordinators.agentProfiles[0].harness"},
		"empty model":        {CoordinatorsConfig{AgentProfiles: []AgentProfile{{ID: "x", Harness: HarnessCodex, Effort: "e"}}}, "coordinators.agentProfiles[0].model"},
		"empty effort":       {CoordinatorsConfig{AgentProfiles: []AgentProfile{{ID: "x", Harness: HarnessCodex, Model: "m"}}}, "coordinators.agentProfiles[0].effort"},
		"negative weight":    {CoordinatorsConfig{AgentProfiles: []AgentProfile{{ID: "x", Harness: HarnessCodex, Model: "m", Effort: "e", Weight: -1}}}, "coordinators.agentProfiles[0].weight"},
		"unknown worker":     {CoordinatorsConfig{AgentProfiles: []AgentProfile{good}, WorkerAgent: AgentSelection{Mode: AgentSelectionFixed, ProfileID: "zz"}}, "coordinators.workerAgent"},
		"unknown root":       {CoordinatorsConfig{AgentProfiles: []AgentProfile{good}, RootWorkerAgent: AgentSelection{Mode: AgentSelectionRandom, ProfileIDs: []string{"a", "zz"}}}, "coordinators.rootWorkerAgent"},
		"empty fixed":        {CoordinatorsConfig{AgentProfiles: []AgentProfile{good}, WorkerAgent: AgentSelection{Mode: AgentSelectionFixed}}, "coordinators.workerAgent"},
		"unknown mode":       {CoordinatorsConfig{AgentProfiles: []AgentProfile{good}, WorkerAgent: AgentSelection{Mode: "shuffle"}}, "coordinators.workerAgent"},
		"inherit with ids":   {CoordinatorsConfig{AgentProfiles: []AgentProfile{good}, WorkerAgent: AgentSelection{Mode: AgentSelectionInherit, ProfileID: "a"}}, "coordinators.workerAgent"},
		"selection w/o list": {CoordinatorsConfig{CoordinatorAgent: AgentSelection{Mode: AgentSelectionRandom, ProfileIDs: nil}, WorkerAgent: AgentSelection{Mode: AgentSelectionFixed, ProfileID: "a"}}, "coordinators.workerAgent"},
	} {
		err := tc.cfg.Validate()
		var perr *AgentProfileError
		if !errors.As(err, &perr) || perr.Field != tc.field {
			t.Errorf("%s: err = %v, want AgentProfileError on %s", name, err, tc.field)
		}
	}
	valid := CoordinatorsConfig{
		AgentProfiles:   []AgentProfile{good, {ID: "b", Harness: HarnessCodex, Model: "m", Effort: "high", Weight: 2}},
		WorkerAgent:     AgentSelection{Mode: AgentSelectionRandom},
		RootWorkerAgent: AgentSelection{Mode: AgentSelectionFixed, ProfileID: "b"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
}

func TestCoordinatorAgentUnknownProfileIsNotAllowed(t *testing.T) {
	cfg := CoordinatorsConfig{CoordinatorAgent: AgentSelection{Mode: AgentSelectionFixed, ProfileID: "zz"}}
	var notAllowed *CoordinatorProfileNotAllowedError
	if err := cfg.Validate(); !errors.As(err, &notAllowed) || notAllowed.ProfileID != "zz" {
		t.Fatalf("err = %v, want COORDINATOR_PROFILE_NOT_ALLOWED for zz", err)
	}
	cfg.CoordinatorAgent = AgentSelection{Mode: AgentSelectionFixed}
	var invalid *AgentProfileError
	if err := cfg.Validate(); !errors.As(err, &invalid) {
		t.Fatalf("a malformed coordinatorAgent is WORKER_PROFILE_INVALID, got %v", err)
	}
}

func TestAgentSelectionCandidates(t *testing.T) {
	profiles := []AgentProfile{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	all, _, _ := AgentSelection{Mode: AgentSelectionRandom}.Candidates(profiles)
	if len(all) != 3 {
		t.Fatalf("an empty random pool means every profile, got %d", len(all))
	}
	some, _, _ := AgentSelection{Mode: AgentSelectionRandom, ProfileIDs: []string{"c", "a", "c"}}.Candidates(profiles)
	if len(some) != 2 || some[0].ID != "c" || some[1].ID != "a" {
		t.Fatalf("pool = %+v, want c then a without duplicates", some)
	}
	if _, unknown, ok := (AgentSelection{Mode: AgentSelectionFixed, ProfileID: "x"}).Candidates(profiles); ok || unknown != "x" {
		t.Fatalf("unknown = %q ok = %v", unknown, ok)
	}
	for _, sel := range []AgentSelection{
		{Mode: AgentSelectionRandom, ProfileIDs: []string{""}},
		{Mode: AgentSelectionRandom, ProfileIDs: []string{"a", ""}},
	} {
		if _, _, ok := sel.Candidates(profiles); ok {
			t.Fatalf("%+v: an empty profile id must not resolve", sel)
		}
		cfg := CoordinatorsConfig{AgentProfiles: profiles, WorkerAgent: sel}
		var perr *AgentProfileError
		if err := cfg.Validate(); !errors.As(err, &perr) {
			t.Fatalf("%+v: err = %v, want WORKER_PROFILE_INVALID", sel, err)
		}
	}
}

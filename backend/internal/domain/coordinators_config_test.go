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

func TestCoordinatorsConfigValidatesAllowedHosts(t *testing.T) {
	ok := CoordinatorsConfig{AllowedHosts: []string{"host-a", "host-b"}, AllowLocalWorkers: true}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a distinct list is valid: %v", err)
	}
	for name, hosts := range map[string][]string{
		"blank":      {"host-a", " "},
		"padded":     {" host-a"},
		"duplicated": {"host-a", "host-a"},
	} {
		if err := (CoordinatorsConfig{AllowedHosts: hosts}).Validate(); err == nil {
			t.Fatalf("%s allowedHosts must be rejected", name)
		}
	}
}

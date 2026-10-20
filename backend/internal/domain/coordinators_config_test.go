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

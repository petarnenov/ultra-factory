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

func TestCoordinatorsConfigRejectsLimitsOutsideTheRolloutCap(t *testing.T) {
	for field, cfg := range map[string]CoordinatorsConfig{
		"maxCoordinators":          {MaxCoordinators: 3},
		"maxWorkersPerCoordinator": {MaxWorkersPerCoordinator: 4},
		"maxWorkers":               {MaxWorkers: -1},
	} {
		var limitsErr *CoordinatorLimitsError
		if err := (ProjectConfig{Coordinators: cfg}).Validate(); !errors.As(err, &limitsErr) || limitsErr.Field != field {
			t.Fatalf("%s: err = %v", field, err)
		}
	}
	if err := (ProjectConfig{Coordinators: CoordinatorsConfig{Enabled: true, MaxCoordinators: 2, MaxWorkersPerCoordinator: 3, MaxWorkers: 6}}).Validate(); err != nil {
		t.Fatalf("the ADR defaults are valid: %v", err)
	}
}

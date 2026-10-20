package domain

import (
	"fmt"
	"strings"
)

// CoordinatorLimits are the effective capacity limits of one project.
type CoordinatorLimits struct {
	Coordinators          int
	WorkersPerCoordinator int
	Workers               int
}

// DefaultCoordinatorLimits are the limits a project gets when it sets none.
var DefaultCoordinatorLimits = CoordinatorLimits{Coordinators: 2, WorkersPerCoordinator: 3, Workers: 6}

// HardCoordinatorLimits are the system maximums a project config may not
// exceed (decided in #408).
var HardCoordinatorLimits = CoordinatorLimits{Coordinators: 4, WorkersPerCoordinator: 6, Workers: 12}

// CoordinatorsConfig is the per-project coordinator gate. A zero limit means
// the ADR default; the feature is disabled until Enabled is true.
type CoordinatorsConfig struct {
	// Enabled allows new coordinator spawns. Restoring a terminated
	// coordinator stays allowed so a rollback can recover.
	Enabled bool `json:"enabled,omitempty"`
	// MaxCoordinators bounds live coordinators per project (default 2).
	MaxCoordinators int `json:"maxCoordinators,omitempty"`
	// MaxWorkersPerCoordinator bounds live workers per coordinator (default 3).
	MaxWorkersPerCoordinator int `json:"maxWorkersPerCoordinator,omitempty"`
	// MaxWorkers bounds live workers per project once a coordinator exists
	// (default 6).
	MaxWorkers int `json:"maxWorkers,omitempty"`
	// AllowedHosts bounds where coordinator-owned workers may run: paired
	// remote host IDs. Empty allows any paired host. It never names the
	// local daemon; local placement is AllowLocalWorkers.
	AllowedHosts []string `json:"allowedHosts,omitempty"`
	// AllowLocalWorkers lets coordinator-owned workers run on this daemon.
	// The default refuses them (COORDINATOR_LOCAL_WORKER_REFUSED).
	AllowLocalWorkers bool `json:"allowLocalWorkers,omitempty"`
}

// Limits resolves the effective limits, filling unset values with defaults.
func (c CoordinatorsConfig) Limits() CoordinatorLimits {
	limits := DefaultCoordinatorLimits
	if c.MaxCoordinators > 0 {
		limits.Coordinators = c.MaxCoordinators
	}
	if c.MaxWorkersPerCoordinator > 0 {
		limits.WorkersPerCoordinator = c.MaxWorkersPerCoordinator
	}
	if c.MaxWorkers > 0 {
		limits.Workers = c.MaxWorkers
	}
	return limits
}

// CoordinatorLimitsError names the config field whose value is outside
// 0..Max. It maps to COORDINATOR_LIMITS_INVALID at the API.
type CoordinatorLimitsError struct {
	Field string
	Max   int
}

func (e *CoordinatorLimitsError) Error() string {
	return fmt.Sprintf("coordinators.%s: must be between 0 and %d", e.Field, e.Max)
}

// Validate rejects limits above the hard maximum or below zero, and a
// resolved workers-per-coordinator above the resolved project total.
func (c CoordinatorsConfig) Validate() error {
	caps := HardCoordinatorLimits
	for _, f := range []struct {
		field    string
		got, max int
	}{
		{"maxCoordinators", c.MaxCoordinators, caps.Coordinators},
		{"maxWorkersPerCoordinator", c.MaxWorkersPerCoordinator, caps.WorkersPerCoordinator},
		{"maxWorkers", c.MaxWorkers, caps.Workers},
	} {
		if f.got < 0 || f.got > f.max {
			return &CoordinatorLimitsError{Field: f.field, Max: f.max}
		}
	}
	if eff := c.Limits(); eff.WorkersPerCoordinator > eff.Workers {
		return &CoordinatorLimitsError{Field: "maxWorkersPerCoordinator", Max: eff.Workers}
	}
	return c.validateAllowedHosts()
}

// validateAllowedHosts requires non-blank, distinct host IDs.
func (c CoordinatorsConfig) validateAllowedHosts() error {
	seen := make(map[string]bool, len(c.AllowedHosts))
	for i, host := range c.AllowedHosts {
		if strings.TrimSpace(host) == "" || host != strings.TrimSpace(host) {
			return fmt.Errorf("coordinators.allowedHosts[%d]: must be a host ID without surrounding spaces", i)
		}
		if seen[host] {
			return fmt.Errorf("coordinators.allowedHosts[%d]: duplicate host %q", i, host)
		}
		seen[host] = true
	}
	return nil
}

// Package agentprofile picks the agent profile of a coordinator or of a
// worker spawn (ADR 0010, amendment 1, decision 8).
package agentprofile

import (
	"context"
	"fmt"
	"math/rand/v2"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

// ReadyFunc reports whether a harness can launch on the target host. A false
// result carries the reason shown in WORKER_PROFILE_UNAVAILABLE.
type ReadyFunc func(ctx context.Context, harness domain.AgentHarness) (ready bool, reason string)

// Intn returns a number in [0, n). Tests inject a seeded source.
type Intn func(n int) int

// Explicit holds the agent flags a caller passed on the spawn.
type Explicit struct {
	Harness domain.AgentHarness
	Model   string
	Effort  string
}

func (e Explicit) any() bool { return e.Harness != "" || e.Model != "" || e.Effort != "" }

func (e Explicit) matches(p domain.AgentProfile) bool {
	return (e.Harness == "" || e.Harness == p.Harness) &&
		(e.Model == "" || e.Model == p.Model) &&
		(e.Effort == "" || e.Effort == p.Effort)
}

// complete reports whether all three flags are set, so one profile can be
// recognised by them.
func (e Explicit) complete() bool { return e.Harness != "" && e.Model != "" && e.Effort != "" }

// Scope says whose agent is picked.
type Scope int

// Scopes of a pick.
const (
	// ScopeCoordinatorWorker is a worker owned by a coordinator.
	ScopeCoordinatorWorker Scope = iota
	// ScopeRootWorker is a worker spawned directly by the root.
	ScopeRootWorker
	// ScopeCoordinator is a coordinator's own agent.
	ScopeCoordinator
)

// Request is one pick.
type Request struct {
	Scope       Scope
	ProjectID   domain.ProjectID
	HostID      string
	Coordinator domain.SessionID
	Profiles    []domain.AgentProfile
	Selection   domain.AgentSelection
	Explicit    Explicit
	Ready       ReadyFunc
	Intn        Intn
}

// Result is the pick. Profile is nil when the spawn keeps its normal agent
// resolution (inherit, or explicit flags that win).
type Result struct {
	Profile *domain.AgentProfile
	// RecordID is the profile ID to record on the session; it is set when
	// Profile is, or when explicit flags match one profile exactly.
	RecordID string
}

// Resolve applies the selection to a spawn.
func Resolve(ctx context.Context, req Request) (Result, error) {
	explicit := req.Explicit
	if req.Selection.IsInherit() {
		return Result{RecordID: exactMatch(req.Profiles, explicit)}, nil
	}
	if req.Scope == ScopeRootWorker && explicit.any() {
		return Result{RecordID: exactMatch(req.Profiles, explicit)}, nil
	}
	selected, unknown := req.Selection.Candidates(req.Profiles)
	if unknown != "" {
		if req.Scope == ScopeCoordinator {
			return Result{}, apierr.Invalid("COORDINATOR_PROFILE_NOT_ALLOWED",
				fmt.Sprintf("Profile %q is not in the project's agent profiles", unknown), map[string]any{"profileId": unknown})
		}
		return Result{}, apierr.Invalid("WORKER_PROFILE_INVALID",
			fmt.Sprintf("Profile %q is not in the project's agent profiles", unknown), map[string]any{"field": "workerAgent"})
	}
	if explicit.any() {
		var matching []domain.AgentProfile
		for _, p := range selected {
			if explicit.matches(p) {
				matching = append(matching, p)
			}
		}
		if len(matching) == 0 {
			return Result{}, notAllowed(req)
		}
		selected = matching
	}
	var ready []domain.AgentProfile
	var excluded []map[string]any
	for _, p := range selected {
		ok, reason := true, ""
		if req.Ready != nil {
			ok, reason = req.Ready(ctx, p.Harness)
		}
		if ok {
			ready = append(ready, p)
			continue
		}
		excluded = append(excluded, map[string]any{"id": p.ID, "reason": reason})
	}
	if len(ready) == 0 {
		return Result{}, unavailable(req, excluded)
	}
	pick := weightedPick(ready, req.Intn)
	return Result{Profile: &pick, RecordID: pick.ID}, nil
}

func exactMatch(profiles []domain.AgentProfile, e Explicit) string {
	if !e.complete() {
		return ""
	}
	for _, p := range profiles {
		if e.matches(p) {
			return p.ID
		}
	}
	return ""
}

func weightedPick(candidates []domain.AgentProfile, intn Intn) domain.AgentProfile {
	if len(candidates) == 1 {
		return candidates[0]
	}
	if intn == nil {
		intn = rand.IntN
	}
	total := 0
	for _, p := range candidates {
		total += p.EffectiveWeight()
	}
	n := intn(total)
	for _, p := range candidates {
		if n < p.EffectiveWeight() {
			return p
		}
		n -= p.EffectiveWeight()
	}
	return candidates[len(candidates)-1]
}

func details(req Request) map[string]any {
	d := map[string]any{"projectId": string(req.ProjectID)}
	if req.HostID != "" {
		d["hostId"] = req.HostID
	}
	if req.Scope == ScopeCoordinatorWorker && req.Coordinator != "" {
		d["coordinatorId"] = string(req.Coordinator)
	}
	return d
}

func notAllowed(req Request) error {
	if req.Scope == ScopeCoordinator {
		return apierr.Invalid("COORDINATOR_PROFILE_NOT_ALLOWED",
			"The agent, model and effort do not match a profile allowed for coordinators", map[string]any{"projectId": string(req.ProjectID)})
	}
	d := details(req)
	delete(d, "hostId")
	return apierr.Conflict("WORKER_PROFILE_NOT_ALLOWED",
		"The agent, model and effort do not match a profile selected for this coordinator", d)
}

func unavailable(req Request, excluded []map[string]any) error {
	d := details(req)
	d["profiles"] = excluded
	if req.Scope == ScopeCoordinator {
		d["scope"] = "coordinator"
		delete(d, "coordinatorId")
	}
	return apierr.Conflict("WORKER_PROFILE_UNAVAILABLE", "No selected agent profile is ready on the target host", d)
}

package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// AgentProfile is one named agent choice (harness, model, effort) that a
// coordinator or one of its workers can run on (ADR 0010, amendment 1).
type AgentProfile struct {
	ID      string       `json:"id"`
	Label   string       `json:"label,omitempty"`
	Harness AgentHarness `json:"harness"`
	Model   string       `json:"model"`
	Effort  string       `json:"effort"`
	// Weight only affects random selection; 0 reads as 1.
	Weight int `json:"weight,omitempty"`
}

// EffectiveWeight is the weight a random pick uses.
func (p AgentProfile) EffectiveWeight() int {
	if p.Weight < 1 {
		return 1
	}
	return p.Weight
}

// DisplayLabel is the label, or the ID when no label is set.
func (p AgentProfile) DisplayLabel() string {
	if p.Label != "" {
		return p.Label
	}
	return p.ID
}

// AgentSelectionMode says how a spawn picks its agent.
type AgentSelectionMode string

// Selection modes. Inherit keeps today's behavior.
const (
	AgentSelectionInherit AgentSelectionMode = "inherit"
	AgentSelectionFixed   AgentSelectionMode = "fixed"
	AgentSelectionRandom  AgentSelectionMode = "random"
)

// AgentSelection picks profiles from the project's shared list. The zero
// value is inherit.
type AgentSelection struct {
	Mode AgentSelectionMode `json:"mode,omitempty"`
	// ProfileID names the one profile of a fixed selection.
	ProfileID string `json:"profileId,omitempty"`
	// ProfileIDs is the pool of a random selection; empty means every
	// profile of the project.
	ProfileIDs []string `json:"profileIds,omitempty"`
}

// IsInherit reports whether the selection leaves the agent unchanged.
func (s AgentSelection) IsInherit() bool {
	return s.Mode == "" || s.Mode == AgentSelectionInherit
}

// Candidates resolves the selected profiles in list order. A name that is
// not in the list is reported as the second result.
func (s AgentSelection) Candidates(profiles []AgentProfile) (selected []AgentProfile, unknown string) {
	byID := make(map[string]AgentProfile, len(profiles))
	for _, p := range profiles {
		byID[p.ID] = p
	}
	switch s.Mode {
	case AgentSelectionFixed:
		p, ok := byID[s.ProfileID]
		if !ok {
			return nil, s.ProfileID
		}
		return []AgentProfile{p}, ""
	case AgentSelectionRandom:
		if len(s.ProfileIDs) == 0 {
			return append([]AgentProfile(nil), profiles...), ""
		}
		seen := map[string]bool{}
		for _, id := range s.ProfileIDs {
			p, ok := byID[id]
			if !ok {
				return nil, id
			}
			if !seen[id] {
				seen[id] = true
				selected = append(selected, p)
			}
		}
	}
	return selected, ""
}

// AgentProfileError names the config or request field that is malformed. It
// maps to WORKER_PROFILE_INVALID at the API.
type AgentProfileError struct {
	Field  string
	Reason string
	// UnknownProfileID is set when the selection names a profile that is not
	// in the list.
	UnknownProfileID string
}

func (e *AgentProfileError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

// CoordinatorProfileNotAllowedError is a coordinator agent selection naming a
// profile outside the list (COORDINATOR_PROFILE_NOT_ALLOWED).
type CoordinatorProfileNotAllowedError struct {
	ProfileID string
}

func (e *CoordinatorProfileNotAllowedError) Error() string {
	return fmt.Sprintf("coordinators.coordinatorAgent: profile %q is not in agentProfiles", e.ProfileID)
}

// ValidateAgentProfiles checks the shared list: unique non-empty IDs, a known
// harness, a model and effort, and a positive weight when set.
func ValidateAgentProfiles(profiles []AgentProfile) error {
	seen := map[string]bool{}
	for i, p := range profiles {
		field := func(name string) string { return fmt.Sprintf("coordinators.agentProfiles[%d].%s", i, name) }
		switch {
		case !validProfileToken(p.ID):
			return &AgentProfileError{Field: field("id"), Reason: "must be a non-empty value without spaces or control characters"}
		case seen[p.ID]:
			return &AgentProfileError{Field: field("id"), Reason: fmt.Sprintf("duplicate id %q", p.ID)}
		case !p.Harness.IsKnown():
			return &AgentProfileError{Field: field("harness"), Reason: fmt.Sprintf("unknown harness %q", p.Harness)}
		case !validProfileToken(p.Model):
			return &AgentProfileError{Field: field("model"), Reason: "must be a non-empty value without spaces or control characters"}
		case !validProfileToken(p.Effort):
			return &AgentProfileError{Field: field("effort"), Reason: "must be a non-empty value without spaces or control characters"}
		case p.Weight < 0:
			return &AgentProfileError{Field: field("weight"), Reason: "must be a positive integer"}
		case len(p.Label) > 100 || strings.ContainsFunc(p.Label, unicode.IsControl):
			return &AgentProfileError{Field: field("label"), Reason: "must be at most 100 characters without control characters"}
		}
		seen[p.ID] = true
	}
	return nil
}

func validProfileToken(v string) bool {
	if v == "" || len(v) > 128 {
		return false
	}
	return !strings.ContainsFunc(v, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// ValidateAgentSelection checks a selection against the profile list. The
// field names the setting in an error.
func ValidateAgentSelection(field string, sel AgentSelection, profiles []AgentProfile) error {
	switch sel.Mode {
	case "", AgentSelectionInherit:
		if sel.ProfileID != "" || len(sel.ProfileIDs) > 0 {
			return &AgentProfileError{Field: field, Reason: "inherit takes no profiles"}
		}
		return nil
	case AgentSelectionFixed:
		if sel.ProfileID == "" || len(sel.ProfileIDs) > 0 {
			return &AgentProfileError{Field: field, Reason: "fixed needs exactly one profileId"}
		}
	case AgentSelectionRandom:
		if sel.ProfileID != "" {
			return &AgentProfileError{Field: field, Reason: "random takes profileIds, not profileId"}
		}
	default:
		return &AgentProfileError{Field: field, Reason: fmt.Sprintf("unknown mode %q", sel.Mode)}
	}
	if _, unknown := sel.Candidates(profiles); unknown != "" {
		return &AgentProfileError{Field: field, Reason: fmt.Sprintf("unknown profile %q", unknown), UnknownProfileID: unknown}
	}
	return nil
}

// CoordinatorAgentEvent audits one live change of a coordinator's worker
// agent selection. Selections are JSON; the actor is always the live root.
type CoordinatorAgentEvent struct {
	ID                int64
	SessionID         SessionID
	PreviousSelection *AgentSelection
	NewSelection      AgentSelection
	ActorSessionID    SessionID
	Origin            string
	CreatedAt         time.Time
}

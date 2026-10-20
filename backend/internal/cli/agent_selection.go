package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// agentSelection mirrors domain.AgentSelection.
type agentSelection struct {
	Mode       string   `json:"mode"`
	ProfileID  string   `json:"profileId,omitempty"`
	ProfileIDs []string `json:"profileIds,omitempty"`
}

// agentProfile mirrors domain.AgentProfile.
type agentProfile struct {
	ID      string `json:"id"`
	Label   string `json:"label,omitempty"`
	Harness string `json:"harness"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	Weight  int    `json:"weight,omitempty"`
}

// parseAgentSelection reads the selection grammar shared by the spawn,
// set-config and coordinator flags: inherit, fixed:<profile-id> or
// random[:<profile-id>,...]. The daemon checks the IDs against the project.
func parseAgentSelection(flag, value string) (*agentSelection, error) {
	value = strings.TrimSpace(value)
	mode, rest, hasRest := strings.Cut(value, ":")
	switch mode {
	case "inherit":
		if hasRest {
			return nil, usageError{fmt.Errorf("%s: inherit takes no profile", flag)}
		}
		return &agentSelection{Mode: "inherit"}, nil
	case "fixed":
		id := strings.TrimSpace(rest)
		if id == "" || strings.Contains(id, ",") {
			return nil, usageError{fmt.Errorf("%s: fixed needs exactly one profile id (fixed:<profile-id>)", flag)}
		}
		return &agentSelection{Mode: "fixed", ProfileID: id}, nil
	case "random":
		sel := &agentSelection{Mode: "random"}
		if hasRest {
			for _, id := range strings.Split(rest, ",") {
				id = strings.TrimSpace(id)
				if id == "" {
					return nil, usageError{fmt.Errorf("%s: empty profile id in %q", flag, value)}
				}
				sel.ProfileIDs = append(sel.ProfileIDs, id)
			}
		}
		return sel, nil
	}
	return nil, usageError{fmt.Errorf("%s: want inherit, fixed:<profile-id> or random[:<profile-id>,...], got %q", flag, value)}
}

// parseAgentProfile reads one --agent-profile value:
// id=<id>,harness=<harness>,model=<model>,effort=<effort>[,label=<text>][,weight=<n>].
func parseAgentProfile(value string) (agentProfile, error) {
	var p agentProfile
	for _, pair := range strings.Split(value, ",") {
		key, val, ok := strings.Cut(pair, "=")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if !ok || val == "" {
			return agentProfile{}, usageError{fmt.Errorf("--agent-profile: %q is not key=value", pair)}
		}
		switch key {
		case "id":
			p.ID = val
		case "label":
			p.Label = val
		case "harness":
			p.Harness = val
		case "model":
			p.Model = val
		case "effort":
			p.Effort = val
		case "weight":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return agentProfile{}, usageError{fmt.Errorf("--agent-profile: weight must be a positive integer, got %q", val)}
			}
			p.Weight = n
		default:
			return agentProfile{}, usageError{fmt.Errorf("--agent-profile: unknown key %q", key)}
		}
	}
	for field, v := range map[string]string{"id": p.ID, "harness": p.Harness, "model": p.Model, "effort": p.Effort} {
		if v == "" {
			return agentProfile{}, usageError{fmt.Errorf("--agent-profile: %s is required", field)}
		}
	}
	return p, nil
}

// parseAgentProfiles reads every repeated --agent-profile value.
func parseAgentProfiles(values []string) ([]agentProfile, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]agentProfile, 0, len(values))
	for _, v := range values {
		p, err := parseAgentProfile(v)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

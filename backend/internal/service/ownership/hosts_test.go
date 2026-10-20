package ownership

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

type testHosts struct {
	paired []HostRef
	self   string
}

func (h testHosts) PairedHosts(context.Context) ([]HostRef, error) { return h.paired, nil }
func (h testHosts) SelfHostID() string                             { return h.self }

func codeOf(err error) string {
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

func TestResolveHostPrefersIDsThenUniqueLabels(t *testing.T) {
	hosts := testHosts{self: "hub", paired: []HostRef{{ID: "h1", Label: "Linux"}, {ID: "Linux", Label: "Other"}, {ID: "h3", Label: "Twin"}, {ID: "h4", Label: "twin"}}}
	paired, _ := hosts.PairedHosts(context.Background())
	for name, tc := range map[string]struct{ in, want, code string }{
		"id":                 {"h1", "h1", ""},
		"label ignores case": {"linux", "h1", ""},
		"id beats label":     {"Linux", "Linux", ""},
		"label match":        {"Other", "Linux", ""},
		"ambiguous label":    {"Twin", "", "COORDINATOR_HOST_NOT_ALLOWED"},
		"local":              {"hub", "", "COORDINATOR_HOST_NOT_ALLOWED"},
		"unknown":            {"nope", "", "COORDINATOR_HOST_NOT_ALLOWED"},
		"blank":              {" ", "", "COORDINATOR_WORKER_LIMIT_INVALID"},
	} {
		got, err := ResolveHost(hosts, paired, tc.in)
		if codeOf(err) != tc.code || got != tc.want {
			t.Fatalf("%s: got %q err=%v, want %q code %q", name, got, err, tc.want, tc.code)
		}
	}
}

func TestNormalizeHostSlotsSortsAndBoundsTheTotal(t *testing.T) {
	hosts := testHosts{paired: []HostRef{{ID: "b", Label: "B"}, {ID: "a", Label: "A"}}}
	got, err := NormalizeHostSlots(context.Background(), hosts, []domain.HostSlot{{HostID: "B", Slots: 1}, {HostID: "a", Slots: 2}}, nil, 3)
	if err != nil || !SameHostSlots(got, []domain.HostSlot{{HostID: "a", Slots: 2}, {HostID: "b", Slots: 1}}) {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if _, err := NormalizeHostSlots(context.Background(), hosts, []domain.HostSlot{{HostID: "a", Slots: 4}}, nil, 3); codeOf(err) != "COORDINATOR_WORKER_LIMIT_INVALID" {
		t.Fatalf("err = %v, want a total above the limit rejected", err)
	}
	if got, err := NormalizeHostSlots(context.Background(), nil, nil, nil, 3); got != nil || err != nil {
		t.Fatalf("no slots need no directory: %v %v", got, err)
	}
}

func TestCheckHostRules(t *testing.T) {
	c1, c2 := domain.SessionID("c1"), domain.SessionID("c2")
	counts := Counts{
		HostSlots:   map[domain.SessionID][]domain.HostSlot{c1: {{HostID: "a", Slots: 1}}},
		ByOwnerHost: map[domain.SessionID]map[string]int{c1: {"a": 1}},
	}
	cfg := domain.CoordinatorsConfig{}
	if err := counts.CheckHost(nil, "zzz", domain.CoordinatorsConfig{AllowedHosts: []string{"a"}}); err != nil {
		t.Fatalf("root-owned: %v", err)
	}
	if err := counts.CheckHost(&c1, "a", cfg); codeOf(err) != "COORDINATOR_HOST_SLOTS_FULL" {
		t.Fatalf("err = %v, want full", err)
	}
	if err := counts.CheckHost(&c1, "b", cfg); codeOf(err) != "COORDINATOR_HOST_NOT_ALLOWED" {
		t.Fatalf("err = %v, want not allowed outside slots", err)
	}
	if err := counts.CheckHost(&c2, "b", cfg); err != nil {
		t.Fatalf("no slots, empty allow list: %v", err)
	}
	if err := counts.CheckHost(&c2, "b", domain.CoordinatorsConfig{AllowedHosts: []string{"a"}}); codeOf(err) != "COORDINATOR_HOST_NOT_ALLOWED" {
		t.Fatalf("err = %v, want not allowed outside allowedHosts", err)
	}
	// A slot lowered under current use keeps the worker and blocks new ones.
	counts.ByOwnerHost[c1]["a"] = 3
	if err := counts.CheckHost(&c1, "a", cfg); codeOf(err) != "COORDINATOR_HOST_SLOTS_FULL" {
		t.Fatalf("err = %v, want full above the slot count", err)
	}
}

func TestEffectivePlacementLimit(t *testing.T) {
	c := domain.SessionID("c1")
	counts := Counts{
		OwnerLimits: map[domain.SessionID]int{c: 4},
		HostSlots:   map[domain.SessionID][]domain.HostSlot{c: {{HostID: "a", Slots: 1}, {HostID: "b", Slots: 1}}},
	}
	limits := domain.CoordinatorLimits{WorkersPerCoordinator: 6}
	if got := counts.EffectivePlacementLimit(c, limits); got != 2 {
		t.Fatalf("with slots = %d, want the slot total 2", got)
	}
	delete(counts.HostSlots, c)
	if got := counts.EffectivePlacementLimit(c, limits); got != 4 {
		t.Fatalf("without slots = %d, want the worker limit 4", got)
	}
}

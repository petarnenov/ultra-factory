package sessionhealth

import (
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func activeRecord() domain.SessionRecord {
	return domain.SessionRecord{
		ID: "s-1", Kind: domain.KindWorker, Mode: domain.SessionModeChat,
		Activity: domain.Activity{State: domain.ActivityActive},
	}
}

func TestDeriveProviderSilenceStalls(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	progress := domain.SessionProgress{ProviderAt: now.Add(-16 * time.Minute)}
	health := Derive(activeRecord(), progress, now, DefaultConfig())
	if health.State != domain.SessionHealthStalled || health.Class != "provider" {
		t.Fatalf("health = %+v, want stalled/provider", health)
	}
	if health.SilentForSeconds != int64((16*time.Minute)/time.Second) {
		t.Fatalf("silentForSeconds = %d", health.SilentForSeconds)
	}
}

func TestDeriveLongToolInsideBudgetIsNotStalled(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	progress := domain.SessionProgress{
		ProviderAt:     now.Add(-45 * time.Minute),
		InFlightTool:   "Bash",
		InFlightToolAt: now.Add(-50 * time.Minute),
	}
	health := Derive(activeRecord(), progress, now, DefaultConfig())
	if health.State != domain.SessionHealthOK || health.Reason != "in_flight_tool" {
		t.Fatalf("health = %+v, want ok/in_flight_tool", health)
	}
}

func TestDeriveLongToolBeyondBudgetStalls(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	progress := domain.SessionProgress{
		InFlightTool:   "Bash",
		InFlightToolAt: now.Add(-61 * time.Minute),
	}
	health := Derive(activeRecord(), progress, now, DefaultConfig())
	if health.State != domain.SessionHealthStalled {
		t.Fatalf("health = %+v, want stalled", health)
	}
}

func TestDeriveShortToolUsesShortBudget(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	progress := domain.SessionProgress{
		InFlightTool:   "Read",
		InFlightToolAt: now.Add(-31 * time.Minute),
	}
	health := Derive(activeRecord(), progress, now, DefaultConfig())
	if health.State != domain.SessionHealthStalled {
		t.Fatalf("health = %+v, want stalled", health)
	}
}

func TestDeriveTerminalSilenceStalls(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	rec := activeRecord()
	rec.Mode = domain.SessionModeTUI
	progress := domain.SessionProgress{TerminalAt: now.Add(-21 * time.Minute)}
	health := Derive(rec, progress, now, DefaultConfig())
	if health.State != domain.SessionHealthStalled || health.Class != "terminal" {
		t.Fatalf("health = %+v, want stalled/terminal", health)
	}
}

func TestDeriveGitProgressPreventsStall(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	progress := domain.SessionProgress{
		ProviderAt: now.Add(-30 * time.Minute),
		GitAt:      now.Add(-2 * time.Minute),
	}
	health := Derive(activeRecord(), progress, now, DefaultConfig())
	if health.State != domain.SessionHealthOK || health.Class != "git" {
		t.Fatalf("health = %+v, want ok/git", health)
	}
}

func TestDeriveBlockedCredentialFailure(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	progress := domain.SessionProgress{
		ProviderAt:    now.Add(-30 * time.Minute),
		BlockedAt:     now.Add(-21 * time.Minute),
		BlockedReason: "git_credential_missing",
	}
	health := Derive(activeRecord(), progress, now, DefaultConfig())
	if health.State != domain.SessionHealthBlocked || health.Reason != "git_credential_missing" {
		t.Fatalf("health = %+v, want blocked/git_credential_missing", health)
	}
}

func TestDeriveWaitingInputBecomesBlockedAfterBudget(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	rec := activeRecord()
	rec.Activity = domain.Activity{State: domain.ActivityWaitingInput, LastActivityAt: now.Add(-21 * time.Minute)}
	health := Derive(rec, domain.SessionProgress{}, now, DefaultConfig())
	if health.State != domain.SessionHealthBlocked || health.Reason != "waiting_on_input" {
		t.Fatalf("health = %+v, want blocked/waiting_on_input", health)
	}
}

func TestDeriveHibernatedChatIsNotStalled(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	hibernated := now.Add(-2 * time.Hour)
	rec := activeRecord()
	rec.HibernatedAt = &hibernated
	health := Derive(rec, domain.SessionProgress{}, now, DefaultConfig())
	if health.State != domain.SessionHealthOK || health.Reason != "hibernated" {
		t.Fatalf("health = %+v, want ok/hibernated", health)
	}
}

func TestDeriveUnreachableIsNotStalled(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	health := DeriveWithReachability(activeRecord(), domain.SessionProgress{}, now, DefaultConfig(), false)
	if health.State != domain.SessionHealthUnreachable {
		t.Fatalf("health = %+v, want unreachable", health)
	}
}

func TestDeriveExitedIsNotStalled(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	rec := activeRecord()
	rec.Activity.State = domain.ActivityExited
	health := Derive(rec, domain.SessionProgress{ProviderAt: now.Add(-2 * time.Hour)}, now, DefaultConfig())
	if health.State != domain.SessionHealthOK || health.Reason != "exited" {
		t.Fatalf("health = %+v, want ok/exited", health)
	}
}

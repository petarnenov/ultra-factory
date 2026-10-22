package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// slotNotices returns the messages delivered to coordinator id so far.
func slotNotices(f *capFixture, id domain.SessionID) []string {
	var out []string
	for i, sent := range f.fc.sent {
		if sent == id {
			out = append(out, f.fc.sentMessages[i])
		}
	}
	return out
}

func TestHostSlotsOnlyChangeNoticesTheCoordinatorWithTheNewSlots(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 4, MaxWorkers: 8})
	c, err := f.spawnHostCoordinator(intp(3))
	if err != nil {
		t.Fatal(err)
	}
	f.fc.sent, f.fc.sentMessages = nil, nil
	slots := CoordinatorLimitsUpdate{HostSlots: &[]domain.HostSlot{{HostID: "Linux", Slots: 2}}}
	if _, err := f.svc.SetCoordinatorLimits(context.Background(), c, slots, rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	got := slotNotices(f, c)
	if len(got) != 1 || !strings.Contains(got[0], "host-linux:2") || !strings.Contains(got[0], "Your worker limit is now 3") {
		t.Fatalf("notices = %q, want one naming host-linux:2 at limit 3", got)
	}
}

func TestHostSlotsChangeWithAWorkerLimitChangeNoticesOnePair(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 4, MaxWorkers: 8})
	c, err := f.spawnHostCoordinator(intp(3))
	if err != nil {
		t.Fatal(err)
	}
	f.fc.sent, f.fc.sentMessages = nil, nil
	pair := CoordinatorLimitsUpdate{WorkerLimit: intp(2), HostSlots: &[]domain.HostSlot{{HostID: "Z600", Slots: 1}}}
	if _, err := f.svc.SetCoordinatorLimits(context.Background(), c, pair, rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	got := slotNotices(f, c)
	if len(got) != 1 || !strings.Contains(got[0], "Your worker limit is now 2") || !strings.Contains(got[0], "host-z600:1") {
		t.Fatalf("notices = %q, want one naming limit 2 and host-z600:1", got)
	}
}

func TestUnchangedHostSlotsSendNoNotice(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 4, MaxWorkers: 8})
	c, err := f.spawnHostCoordinator(intp(3), domain.HostSlot{HostID: "host-linux", Slots: 2})
	if err != nil {
		t.Fatal(err)
	}
	f.fc.sent, f.fc.sentMessages = nil, nil
	same := CoordinatorLimitsUpdate{HostSlots: &[]domain.HostSlot{{HostID: "Linux", Slots: 2}}}
	if _, err := f.svc.SetCoordinatorLimits(context.Background(), c, same, rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	if got := slotNotices(f, c); len(got) != 0 {
		t.Fatalf("unchanged slots noticed: %q", got)
	}
}

func TestHostSlotNoticeWakesAHibernatedCoordinator(t *testing.T) {
	f := newHostFixture(t, domain.CoordinatorsConfig{MaxWorkersPerCoordinator: 4, MaxWorkers: 8})
	c, err := f.spawnHostCoordinator(intp(3))
	if err != nil {
		t.Fatal(err)
	}
	asleep := time.Now().UTC()
	rec := f.store.sessions[c]
	rec.HibernatedAt = &asleep
	f.store.sessions[c] = rec
	f.fc.sent, f.fc.sentMessages = nil, nil
	slots := CoordinatorLimitsUpdate{HostSlots: &[]domain.HostSlot{{HostID: "Linux", Slots: 1}}}
	if _, err := f.svc.SetCoordinatorLimits(context.Background(), c, slots, rootCaller("ao-root")); err != nil {
		t.Fatal(err)
	}
	if got := slotNotices(f, c); len(got) != 1 {
		t.Fatalf("hibernated coordinator notices = %q, want one", got)
	}
}

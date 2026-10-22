package remotereports

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A host that answers pullerAcks confirms per-puller acknowledgement, so the
// acknowledgement names this daemon as the puller.
func TestPollAcknowledgesAsThePullerOnAHostWithPerPullerAcks(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	raw, _ := json.Marshal(map[string]any{"reports": []map[string]any{needsInput("rpt_1", now)}, "pullerAcks": true})
	host.pending["widgets-3"] = string(raw)
	observer := newTestObserver(newMemStore(link("host-build", "widgets-3")), &fakeDeliverer{}, host)

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if observer.legacyWarned["host-build"] {
		t.Fatal("a per-puller host must not be reported as legacy")
	}
	acks := 0
	for _, r := range host.seen() {
		if r.method == http.MethodPost && r.path == "/api/v1/reports/acknowledge" {
			acks++
			if !strings.Contains(r.body, `"pullerId":"host-local"`) {
				t.Fatalf("ack body = %s, want the puller id", r.body)
			}
		}
	}
	if acks != 1 {
		t.Fatalf("acks = %d, want 1", acks)
	}
}

// A host that predates per-puller acks ignores the puller ID and rejects
// unknown body fields, so the observer must not send it there. It warns once.
func TestPollFallsBackToFirstDeliveryOnAHostWithoutPerPullerAcks(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	host := newFakeHost(t, "host-build", "pw")
	host.pending["widgets-3"] = reportsJSON(needsInput("rpt_1", now))
	observer := newTestObserver(newMemStore(link("host-build", "widgets-3")), &fakeDeliverer{}, host)

	for range 2 {
		if err := observer.Poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !observer.legacyWarned["host-build"] {
		t.Fatal("an old host must be recorded as legacy")
	}
	acks := host.acks()
	if len(acks) == 0 {
		t.Fatal("an old host still gets acknowledgements")
	}
	for _, r := range host.seen() {
		if r.method == http.MethodPost && strings.Contains(r.body, "pullerId") {
			t.Fatalf("ack to an old host carried a puller id: %s", r.body)
		}
	}
}

// Reports the host expired before this daemon pulled are remembered as seen
// and sent back on the next pull, so the host subtracts them.
func TestPollRecordsExpiredBeforePullAsSeen(t *testing.T) {
	host := newFakeHost(t, "host-build", "pw")
	host.pending["widgets-3"] = `{"reports":[],"pullerAcks":true,"expiredBeforePull":3,"expiredTotal":3}`
	observer := newTestObserver(newMemStore(link("host-build", "widgets-3")), &fakeDeliverer{}, host)

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := observer.expiredSeenFor(linkRef{hostID: "host-build", session: "widgets-3"}); got != 3 {
		t.Fatalf("expired seen = %d, want 3", got)
	}
}

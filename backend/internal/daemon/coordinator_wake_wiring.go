package daemon

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type coordinatorKindStore interface {
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
}

// wakeOnCoordinatorTermination wakes report delivery after a coordinator
// terminates; the SQL trigger's rows fire no OnCreated hook, so CDC does.
func wakeOnCoordinatorTermination(ctx context.Context, b *cdc.Broadcaster, store coordinatorKindStore, log *slog.Logger, wake func()) (unsubscribe func()) {
	return b.Subscribe(func(e cdc.Event) {
		if e.Type != cdc.EventSessionUpdated || e.SessionID == "" {
			return
		}
		var p struct {
			IsTerminated bool `json:"isTerminated"`
		}
		if json.Unmarshal(e.Payload, &p) != nil || !p.IsTerminated {
			return
		}
		rec, ok, err := store.GetSession(ctx, domain.SessionID(e.SessionID))
		if err != nil {
			// CDC does not replay; wake anyway so a scan runs.
			log.Warn("coordinator wake: session lookup failed", "session", e.SessionID, "err", err)
			wake()
			return
		}
		if ok && rec.Kind == domain.KindCoordinator {
			wake()
		}
	})
}

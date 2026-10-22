package ownership

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Notifier delivers a system message into a local session. The session
// manager's Send satisfies it; Send wakes a hibernated chat before delivery.
type Notifier interface {
	Send(context.Context, domain.SessionID, string, *ports.SpawnAttachment) error
}

// Change is one committed owner write that the parties must hear about.
type Change struct {
	Worker   domain.SessionID
	Previous *domain.SessionID
	Next     *domain.SessionID
	// PreviousLive reports whether Previous is a live coordinator. A root or
	// terminated previous owner gets no notice.
	PreviousLive bool
	// Local reports whether Worker is a session on this daemon. A remote
	// worker has no local agent to message.
	Local bool
}

// Notify sends the notices for change after the write committed. Delivery
// failures are logged, never returned: the owner write already happened.
func Notify(ctx context.Context, n Notifier, logger *slog.Logger, change Change) {
	if n == nil || sameOwner(change.Previous, change.Next) {
		return
	}
	if change.Next != nil {
		send(ctx, n, logger, *change.Next, AssignedMessage(change.Worker))
	}
	if change.PreviousLive && change.Previous != nil {
		send(ctx, n, logger, *change.Previous, RemovedMessage(change.Worker, change.Next))
	}
	if change.Local {
		send(ctx, n, logger, change.Worker, WorkerMessage(change.Next))
	}
}

// AssignedMessage tells a coordinator it now owns worker.
func AssignedMessage(worker domain.SessionID) string {
	return fmt.Sprintf("You now own worker %s. Reports from it will reach you.", worker)
}

// RemovedMessage tells a previous coordinator it no longer owns worker.
func RemovedMessage(worker domain.SessionID, next *domain.SessionID) string {
	return fmt.Sprintf("Ownership removed: worker %s is now owned by %s. Stop acting on it.", worker, ownerLabel(next))
}

// WorkerMessage tells a worker who its escalation target is now.
func WorkerMessage(next *domain.SessionID) string {
	if next == nil {
		return "Your owner is now the project root. Escalate to the root from now on."
	}
	return fmt.Sprintf("Your owner is now coordinator %s. Escalate to it from now on.", *next)
}

func ownerLabel(owner *domain.SessionID) string {
	if owner == nil {
		return "the project root"
	}
	return "coordinator " + string(*owner)
}

func sameOwner(a, b *domain.SessionID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func send(ctx context.Context, n Notifier, logger *slog.Logger, id domain.SessionID, message string) {
	if err := n.Send(ctx, id, message, nil); err != nil && logger != nil {
		logger.Warn("ownership notice not delivered", "session", id, "error", err)
	}
}

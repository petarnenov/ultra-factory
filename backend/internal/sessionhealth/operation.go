package sessionhealth

import (
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ReasonAgentOperationStuck marks a session whose wake, exit, or switch has held
// it longer than Config.OperationAfter.
const ReasonAgentOperationStuck = "agent_operation_stuck"

// OperationSource reads and abandons the in-flight agent operation of a session.
type OperationSource interface {
	AgentOperation(domain.SessionID) (domain.SessionAgentOperation, bool)
	// AbandonAgentOperation asks the operation to stop and reports whether it
	// could be asked.
	AbandonAgentOperation(domain.SessionID) bool
}

// WithOperation overlays a stuck transition on health. A session that is
// hibernated or otherwise looks ok is still blocked while a transition hangs.
func WithOperation(health domain.SessionHealth, op *domain.SessionAgentOperation, now time.Time, cfg Config) domain.SessionHealth {
	if op == nil || op.StartedAt.IsZero() {
		return health
	}
	held := nonNegative(now.UTC().Sub(op.StartedAt))
	if held < cfg.OperationAfter {
		return health
	}
	health.State = domain.SessionHealthBlocked
	health.Reason = ReasonAgentOperationStuck
	health.Class = op.Kind
	health.LastAt = timePointer(op.StartedAt)
	health.SilentForSeconds = int64(held / time.Second)
	health.ThresholdSeconds = int64(cfg.OperationAfter / time.Second)
	health.Since = timePointer(op.StartedAt.Add(cfg.OperationAfter))
	return health
}

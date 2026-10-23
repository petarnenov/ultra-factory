package daemon

import (
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

// healthOperations feeds the health observer the session manager's in-flight
// agent operations.
type healthOperations struct {
	manager interface {
		AgentOperationInProgress(domain.SessionID) (sessionmanager.AgentOperationStatus, bool)
		AbandonAgentOperation(domain.SessionID) bool
	}
}

func (h healthOperations) AgentOperation(id domain.SessionID) (domain.SessionAgentOperation, bool) {
	status, held := h.manager.AgentOperationInProgress(id)
	return domain.SessionAgentOperation{Kind: status.Kind, StartedAt: status.StartedAt}, held
}

func (h healthOperations) AbandonAgentOperation(id domain.SessionID) bool {
	return h.manager.AbandonAgentOperation(id)
}

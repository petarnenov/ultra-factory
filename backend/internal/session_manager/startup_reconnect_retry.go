package sessionmanager

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var startupChatReconnectRetrySchedule = []time.Duration{
	2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second,
	30 * time.Second, time.Minute, time.Minute,
}

func retryableStartupChatReconnect(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ports.ErrChatRecoveryInconclusive)
}

func (m *Manager) scheduleStartupChatReconnect(rec domain.SessionRecord, err error) bool {
	if domain.NormalizeSessionMode(rec.Mode) != domain.SessionModeChat || !retryableStartupChatReconnect(err) {
		return false
	}
	m.statusRecoveryMu.Lock()
	if _, exists := m.startupChatReconnectRetries[rec.ID]; exists {
		m.statusRecoveryMu.Unlock()
		return true
	}
	m.startupChatReconnectRetries[rec.ID] = struct{}{}
	m.statusRecoveries[rec.ID] = statusRecovery{pending: true}
	m.statusRecoveryRevision++
	m.statusRecoveryMu.Unlock()
	m.runInBackground(func() { m.retryStartupChatReconnect(rec, err) })
	return true
}

func (m *Manager) retryStartupChatReconnect(rec domain.SessionRecord, lastErr error) {
	defer func() {
		m.statusRecoveryMu.Lock()
		delete(m.startupChatReconnectRetries, rec.ID)
		m.statusRecoveryMu.Unlock()
	}()
	delays := m.startupChatReconnectRetryDelays
	if delays == nil {
		delays = startupChatReconnectRetrySchedule
	}
	ctx := m.backgroundContext
	for _, delay := range delays {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			m.finishStatusRecovery(ctx, rec, lastErr)
			return
		case <-timer.C:
		}
		current, found, err := m.store.GetSession(ctx, rec.ID)
		if err != nil {
			lastErr = err
			continue
		}
		if !found || current.IsTerminated || current.HibernatedAt != nil ||
			current.ProvisionState.WithDefault() != domain.SessionProvisionReady ||
			domain.NormalizeSessionMode(current.Mode) != domain.SessionModeChat {
			m.finishStatusRecovery(ctx, rec, lastErr)
			return
		}
		rec = current
		if m.chat != nil && m.chat.HasLiveChatController(rec.ID) {
			m.finishStatusRecovery(ctx, rec, nil)
			return
		}
		acquired, acquireErr := m.beginAgentOperations(ctx, []domain.SessionID{rec.ID}, agentOperationReconcile)
		if acquireErr != nil || len(acquired) == 0 {
			continue
		}
		m.beginStatusRecovery(rec.ID)
		attemptErr := func() error {
			defer m.endAgentOperation(rec.ID, agentOperationReconcile)
			recoveryCtx, cancel := context.WithTimeout(ctx, m.statusVerificationLimit)
			defer cancel()
			return m.checkSessionHealth(recoveryCtx, rec)
		}()
		if attemptErr == nil || !retryableStartupChatReconnect(attemptErr) {
			m.finishStatusRecovery(ctx, rec, attemptErr)
			return
		}
		lastErr = attemptErr
	}
	m.finishStatusRecovery(ctx, rec, lastErr)
}

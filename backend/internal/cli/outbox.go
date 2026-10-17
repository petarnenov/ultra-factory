package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"syscall"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/outbox"
)

// undelivered reports whether err proves the request never reached a daemon:
// the run-file check found none, or nothing accepted the connection. Only then
// may the request be queued, because delivering it later cannot apply it twice.
// A timeout or reset after the request was sent proves nothing.
func undelivered(err error) bool {
	return errors.Is(err, errDaemonNotRunning) || errors.Is(err, syscall.ECONNREFUSED)
}

// queueForDaemon writes a request the local daemon did not receive to the
// outbox (see package outbox); the daemon delivers it when it is back.
func (c *commandContext) queueForDaemon(kind, path, sessionID string, body any, observedAt time.Time) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	entry := outbox.Entry{
		Version:   outbox.Version,
		Kind:      kind,
		Method:    http.MethodPost,
		Path:      path,
		Body:      data,
		SessionID: sessionID,
		CreatedAt: c.deps.Now().UTC(),
	}
	if !observedAt.IsZero() {
		entry.ObservedAt = observedAt.UTC()
	}
	return outbox.Write(outbox.Dirs(cfg.DataDir), entry)
}

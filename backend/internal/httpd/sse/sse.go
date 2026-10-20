// Package sse holds the write path shared by the host's SSE handlers.
package sse

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// WriteTimeout bounds one frame write plus flush, so a peer that stops
// reading cannot pin the handler. The server-wide WriteTimeout stays off.
var WriteTimeout = 30 * time.Second

// Send writes one SSE frame and flushes it under a fresh write deadline.
// A non-nil error means the stream is dead.
func Send(w http.ResponseWriter, format string, args ...any) error {
	rc := http.NewResponseController(w)
	// ErrNotSupported only means the writer cannot take a deadline.
	if err := rc.SetWriteDeadline(time.Now().Add(WriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return err
	}
	if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

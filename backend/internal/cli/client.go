package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
)

// commandTimeout bounds a mutating daemon call. Spawns do real work (git
// worktree add, tmux launch, hook install), so it is generous compared to the
// status probe timeout.
const commandTimeout = 2 * time.Minute

// maxDrainedBodyBytes bounds how much of an unused response body the CLI
// discards for keep-alive reuse without an unbounded read.
const maxDrainedBodyBytes = 4 << 10

// apiError is the subset of the daemon's JSON error envelope the CLI surfaces.
// RequestID is surfaced so a failed command can be correlated with daemon logs.
type apiError struct {
	Message   string `json:"message"`
	Code      string `json:"code"`
	RequestID string `json:"requestId"`
}

type apiResponseError struct {
	StatusCode int
	ErrorBody  apiError
}

var errDaemonUnavailable = errors.New("AO daemon unavailable")

// errDaemonNotRunning distinguishes a missing/stale run file from transport
// failures so hooks can keep expected restart noise out of the agent's stderr.
var errDaemonNotRunning = errors.New("AO daemon is not running")

// daemonUnavailableError keeps the established user-facing diagnostics while
// giving the few idempotent CLI operations that can safely retry a typed signal.
// Most commands continue returning this error immediately through doJSON.
type daemonUnavailableError struct {
	message string
	cause   error
}

func (e daemonUnavailableError) Error() string { return e.message }

func (e daemonUnavailableError) Unwrap() error { return e.cause }

func (e daemonUnavailableError) Is(target error) bool {
	return target == errDaemonUnavailable || errors.Is(e.cause, target)
}

// daemonResponseBody marks read failures for retry by idempotent calls. Keeping
// the marker at the reader boundary leaves JSON syntax and value errors intact.
type daemonResponseBody struct{ io.ReadCloser }

func (b daemonResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, daemonUnavailableError{message: err.Error(), cause: err}
	}
	return n, err
}

func (e apiResponseError) Error() string {
	if e.ErrorBody.Message == "" {
		return fmt.Sprintf("daemon returned HTTP %d", e.StatusCode)
	}
	return e.ErrorBody.String()
}

// String renders the envelope for the user: "<message> (<code>) [request <id>]",
// omitting whichever parts the daemon left empty.
func (e apiError) String() string {
	msg := e.Message
	if e.Code != "" {
		msg = fmt.Sprintf("%s (%s)", msg, e.Code)
	}
	if e.RequestID != "" {
		msg = fmt.Sprintf("%s [request %s]", msg, e.RequestID)
	}
	return msg
}

// getJSON sends GET /api/v1/<path> to the running daemon and decodes a 2xx
// response into out. A missing daemon or non-2xx API envelope is rendered the
// same way as mutating calls.
func (c *commandContext) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

// postJSON sends body as JSON to POST /api/v1/<path> on the running daemon and
// decodes a 2xx response into out (out may be nil). A non-2xx response becomes
// an error built from the API error envelope. A missing run-file or a stale one
// (dead PID) yields a clear "not running" message rather than a
// connection-refused dump.
func (c *commandContext) postJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPost, path, body, out)
}

// patchJSON sends body as JSON to PATCH /api/v1/<path> on the running daemon
// and decodes a 2xx response into out.
func (c *commandContext) patchJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPatch, path, body, out)
}

// putJSON sends body as JSON to PUT /api/v1/<path> on the running daemon and
// decodes a 2xx response into out.
func (c *commandContext) putJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPut, path, body, out)
}

// deleteJSON sends DELETE /api/v1/<path> to the running daemon and decodes a
// 2xx response into out.
func (c *commandContext) deleteJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodDelete, path, nil, out)
}

func (c *commandContext) doJSON(ctx context.Context, method, path string, body, out any) error {
	return c.doJSONPath(ctx, method, "/api/v1/"+path, body, out)
}

// postLoopbackJSON always targets this machine's daemon, even when the command
// runs with --host: loopback-only routes (telemetry) never reach a remote host.
func (c *commandContext) postLoopbackJSON(ctx context.Context, path string, body any) error {
	return c.localContext().doJSONPath(ctx, http.MethodPost, path, body, nil)
}

func (c *commandContext) doJSONPath(ctx context.Context, method, path string, body, out any) error {
	return c.doJSONPathWithHeaders(ctx, method, path, body, out, nil)
}

func (c *commandContext) doJSONPathWithHeaders(
	ctx context.Context,
	method, path string,
	body, out any,
	headers map[string]string,
) error {
	return c.doJSONPathWithHeadersAndTimeout(ctx, method, path, body, out, headers, commandTimeout)
}

// ensureLocalDaemon decides whether the daemon recorded in the run-file is up
// before a request is sent to its port. A visible live PID is enough. A PID
// that looks dead is not: agents run commands in sandboxes with a private PID
// namespace (Codex uses bwrap on Linux) where the daemon's PID is invisible,
// and treating that as "stopped" lost their reports and hook events. Then the
// daemon's own answer on its loopback port, which the sandbox shares, decides;
// the result is cached for the rest of the invocation.
func (c *commandContext) ensureLocalDaemon(ctx context.Context, runFilePath string, info runfile.Info) error {
	if c.deps.ProcessAlive(info.PID) {
		return nil
	}
	key := fmt.Sprintf("%d/%d/%s", info.PID, info.Port, info.InstanceID)
	if c.verifiedDaemon == key {
		return nil
	}
	if _, answered, err := c.probeRunFileOwner(ctx, info); answered && err == nil {
		c.verifiedDaemon = key
		return nil
	}
	if inAgentSession() {
		return daemonUnavailableError{message: sandboxedDaemonMessage(info), cause: errDaemonNotRunning}
	}
	return daemonUnavailableError{message: fmt.Sprintf("AO daemon is not running (stale run-file at %s) — start it with `ao start`", runFilePath), cause: errDaemonNotRunning}
}

// inAgentSession reports whether this command runs inside an AO agent session,
// which the daemon marks with AO_SESSION_ID.
func inAgentSession() bool {
	return strings.TrimSpace(os.Getenv("AO_SESSION_ID")) != ""
}

// sandboxedDaemonMessage explains an unreachable daemon to an agent. Neither the
// run-file's PID nor its port is reachable, which inside an agent session most
// often means a sandbox hides the daemon rather than that it stopped. Telling
// the agent to start it made agents replace a live daemon with their own.
func sandboxedDaemonMessage(info runfile.Info) string {
	return fmt.Sprintf("AO daemon is not reachable from this agent session: its process (pid %d) is not visible and its port %d does not answer. "+
		"It is most likely running outside your sandbox: run this command again outside the sandbox (request escalation). "+
		"Do not run `ao start`, `ao stop` or `ao daemon` from an agent session.", info.PID, info.Port)
}

func (c *commandContext) doJSONPathWithHeadersAndTimeout(
	ctx context.Context,
	method, path string,
	body, out any,
	headers map[string]string,
	timeout time.Duration,
) error {
	var remote *remoteTarget
	var url string
	if c.targetsRemote() {
		target, err := c.resolveRemoteTarget(ctx)
		if err != nil {
			return err
		}
		remote = target
		url = remote.baseURL + path
	} else {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		info, err := runfile.Read(cfg.RunFilePath)
		if err != nil {
			return err
		}
		if info == nil {
			return daemonUnavailableError{message: "AO daemon is not running — start it with `ao start`", cause: errDaemonNotRunning}
		}
		if err := c.ensureLocalDaemon(ctx, cfg.RunFilePath, *info); err != nil {
			return err
		}
		url = fmt.Sprintf("http://%s:%d%s", config.LoopbackHost, info.Port, path)
	}

	var reader io.Reader = http.NoBody
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader) // #nosec G704 -- loopback daemon, or a saved remote host verified by resolveRemoteTarget; path is an internal API route.
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	if remote != nil {
		req.Header.Set("Authorization", "Bearer "+remote.password)
		req.Header.Set("X-AO-Expected-Host-ID", remote.hostID)
	} else if sideID := os.Getenv("AO_SIDE_CONVERSATION_ID"); sideID != "" {
		req.Header.Set("X-AO-Side-Conversation", sideID)
	}

	// Reuse the injected client's transport (keeps it stubbable in tests) but
	// give daemon API calls far more headroom than the 2s status-probe timeout.
	client := *c.deps.HTTPClient
	client.Timeout = timeout
	if remote != nil {
		client = c.remoteHTTPClient(timeout)
	}
	resp, err := client.Do(req) // #nosec G704 -- request target is the daemon URL resolved above.
	if err != nil {
		if remote != nil {
			return daemonUnavailableError{message: fmt.Sprintf("call remote host %q: %v", remote.label, err), cause: err}
		}
		return daemonUnavailableError{message: fmt.Sprintf("call daemon: %v", err), cause: err}
	}
	resp.Body = daemonResponseBody{ReadCloser: resp.Body}
	defer func() { _ = resp.Body.Close() }()

	if remote != nil && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return fmt.Errorf("remote host %q answered with a redirect (HTTP %d); refusing to follow it with the saved credential", remote.label, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e apiError
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if remote != nil && resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("remote host %q rejected the saved password; update it in the desktop app under Settings → Remote hosts: %w", remote.label, apiResponseError{StatusCode: resp.StatusCode, ErrorBody: e})
		}
		return apiResponseError{StatusCode: resp.StatusCode, ErrorBody: e}
	}
	if out == nil {
		// Explicitly bodyless call (telemetry/activity hooks, fire-and-forget
		// posts). Drain only a bounded remainder so the connection can be
		// reused without an unbounded read.
		_, _ = io.CopyN(io.Discard, resp.Body, maxDrainedBodyBytes)
		return nil
	}
	// A 204 carries no body by contract; a zero-value out is the legitimate
	// result. Any other 2xx with a required decoded result must carry a
	// JSON document — an empty body is a broken contract, not success.
	if resp.StatusCode == http.StatusNoContent {
		_, _ = io.CopyN(io.Discard, resp.Body, maxDrainedBodyBytes)
		return nil
	}
	// Peek at the first non-whitespace byte before decoding: an empty body
	// surfaces as io.EOF, but a literal JSON null decodes successfully into
	// a zero value. Both are a broken contract for a required result, so
	// both are rejected here rather than silently succeeding.
	br := bufio.NewReader(resp.Body)
	for {
		b, err := br.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("decode response: missing required response body (HTTP %d %s %s): %w", resp.StatusCode, method, path, err)
			}
			return fmt.Errorf("decode response: %w", err)
		}
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		if b == 'n' {
			if rest, err := br.Peek(3); err == nil && string(rest) == "ull" {
				return fmt.Errorf("decode response: missing required response body (HTTP %d %s %s): null response body", resp.StatusCode, method, path)
			}
		}
		if err := br.UnreadByte(); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		break
	}
	if err := json.NewDecoder(br).Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("decode response: missing required response body (HTTP %d %s %s): %w", resp.StatusCode, method, path, err)
		}
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

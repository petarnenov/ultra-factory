package remotereports

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

// errSessionGone reports that the remote daemon answered SESSION_NOT_FOUND
// for the linked worker. Any other 404, such as an older remote AO without
// the pull routes, is an ordinary error and never drops a link.
var errSessionGone = errors.New("remote session not found")

// errCredentialRefused reports a 401 or 429 from the remote host: the saved
// password is wrong or the host is locking this machine out. The observer
// stops sending it until the saved entry changes.
var errCredentialRefused = errors.New("remote host refused the saved password; update it in the desktop app under Settings → Remote hosts")

// Limits on what one remote host can make this daemon read and deliver.
const (
	maxResponseBytes  = 4 << 20
	maxReportsPerPull = reportsvc.MaxAcknowledgeReports
	maxReportIDLength = 128
	maxReferenceRunes = 2048
	maxLabelRunes     = 200
)

// hostClient makes authenticated requests to one identity-checked host.
type hostClient struct {
	http     http.Client
	base     string
	hostID   string
	password string
	label    string
}

// The wire shapes below mirror the remote daemon's DTOs
// (internal/httpd/controllers/dto.go) without importing the controllers.
type wireReportOutput struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	Label     string `json:"label,omitempty"`
}

type wireReport struct {
	ID          string             `json:"id"`
	State       string             `json:"state,omitempty"`
	Note        string             `json:"note,omitempty"`
	Message     string             `json:"message,omitempty"`
	Outputs     []wireReportOutput `json:"outputs,omitempty"`
	CreatedAt   time.Time          `json:"createdAt"`
	RepeatCount int64              `json:"repeatCount"`
}

type wireAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// pendingResult is one worker's due reports as this daemon accepts them.
type pendingResult struct {
	reports []domain.ReportRecord
	// rejected are due reports that break the content contract or the size
	// limits. They are acknowledged without delivery so they cannot block
	// the worker's other reports or keep its link alive forever.
	rejected []string
	// more is true when the remote holds further pending reports: not yet
	// due, or beyond this pull's limit.
	more bool
}

// pending returns the worker's due, still-pending reports.
func (h *hostClient) pending(ctx context.Context, sessionID domain.SessionID) (pendingResult, error) {
	var body struct {
		Reports     []wireReport `json:"reports"`
		MorePending bool         `json:"morePending"`
	}
	path := "/api/v1/reports/pending?sessionId=" + url.QueryEscape(string(sessionID))
	if err := h.do(ctx, http.MethodGet, path, nil, &body); err != nil {
		return pendingResult{}, err
	}
	result := pendingResult{more: body.MorePending}
	if len(body.Reports) > maxReportsPerPull {
		body.Reports = body.Reports[:maxReportsPerPull]
		result.more = true
	}
	for _, wire := range body.Reports {
		report, ok := acceptReport(wire)
		if !ok {
			if wire.ID != "" && len(wire.ID) <= maxReportIDLength {
				result.rejected = append(result.rejected, wire.ID)
			}
			continue
		}
		result.reports = append(result.reports, report)
	}
	return result, nil
}

// acceptReport converts a wire report that satisfies the same content contract
// a local report does, plus limits on what reaches the orchestrator's prompt.
func acceptReport(wire wireReport) (domain.ReportRecord, bool) {
	if wire.ID == "" || len(wire.ID) > maxReportIDLength {
		return domain.ReportRecord{}, false
	}
	outputs := make([]domain.ReportOutput, len(wire.Outputs))
	for i, output := range wire.Outputs {
		if utf8.RuneCountInString(output.Reference) > maxReferenceRunes || utf8.RuneCountInString(output.Label) > maxLabelRunes {
			return domain.ReportRecord{}, false
		}
		outputs[i] = domain.ReportOutput{Kind: domain.ReportOutputKind(output.Kind), Reference: output.Reference, Label: output.Label}
	}
	state := domain.ReportState(wire.State)
	if domain.ValidateReportContent(state, wire.Note, wire.Message, outputs) != nil {
		return domain.ReportRecord{}, false
	}
	repeat := wire.RepeatCount
	if repeat < 1 {
		repeat = 1
	}
	return domain.ReportRecord{
		ID: wire.ID, State: state, Note: wire.Note, Message: wire.Message,
		Outputs: outputs, CreatedAt: wire.CreatedAt, RepeatCount: repeat,
	}, true
}

// acknowledge settles pulled reports on the remote host in chunks the remote
// accepts.
func (h *hostClient) acknowledge(ctx context.Context, sessionID domain.SessionID, ids []string) error {
	for start := 0; start < len(ids); start += reportsvc.MaxAcknowledgeReports {
		end := min(start+reportsvc.MaxAcknowledgeReports, len(ids))
		request := struct {
			SessionID string   `json:"sessionId"`
			ReportIDs []string `json:"reportIds"`
		}{SessionID: string(sessionID), ReportIDs: ids[start:end]}
		if err := h.do(ctx, http.MethodPost, "/api/v1/reports/acknowledge", request, nil); err != nil {
			return err
		}
	}
	return nil
}

// terminated reports whether the remote worker session has ended.
func (h *hostClient) terminated(ctx context.Context, sessionID domain.SessionID) (bool, error) {
	var body struct {
		Session struct {
			IsTerminated bool `json:"isTerminated"`
		} `json:"session"`
	}
	if err := h.do(ctx, http.MethodGet, "/api/v1/sessions/"+url.PathEscape(string(sessionID)), nil, &body); err != nil {
		return false, err
	}
	return body.Session.IsTerminated, nil
}

// do sends one request carrying the saved credential and the expected host ID,
// which the remote daemon checks so a moved address cannot answer for it.
// Errors never include the credential.
func (h *hostClient) do(ctx context.Context, method, path string, in, out any) error {
	var reader io.Reader = http.NoBody
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.base+path, reader) // #nosec G704 -- saved, identity-checked remote host; path is an internal API route.
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+h.password)
	req.Header.Set("X-AO-Expected-Host-ID", h.hostID)
	resp, err := h.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	limited := io.LimitReader(resp.Body, maxResponseBytes)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var apiErr wireAPIError
		_ = json.NewDecoder(limited).Decode(&apiErr)
		switch {
		case resp.StatusCode == http.StatusNotFound && apiErr.Code == "SESSION_NOT_FOUND":
			return errSessionGone
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests:
			return errCredentialRefused
		}
		return fmt.Errorf("%s %s: HTTP %d %s", method, path, resp.StatusCode, apiErr.Code)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(limited).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}

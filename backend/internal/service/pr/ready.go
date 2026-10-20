package pr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	coordownership "github.com/aoagents/agent-orchestrator/backend/internal/service/ownership"
)

// readyStore is the storage SetReady needs beyond the base action store.
type readyStore interface {
	GetSession(ctx context.Context, id domain.SessionID) (domain.SessionRecord, bool, error)
	ListPRsBySession(ctx context.Context, id domain.SessionID) ([]domain.PullRequest, error)
	RecordSessionPRReadyEvent(ctx context.Context, ev domain.SessionPRReadyEvent) error
}

// requireReadyAuthority allows only the root and the owning coordinator; a
// worker must not bypass its own gate. Anonymous callers (people) pass.
func requireReadyAuthority(ctx context.Context, store readyStore, target domain.SessionRecord, requestedBy domain.SessionID) error {
	if requestedBy == "" {
		return nil
	}
	caller, found, err := store.GetSession(ctx, requestedBy)
	if err != nil {
		return fmt.Errorf("load ready requester %s: %w", requestedBy, err)
	}
	if found {
		if coordownership.IsLiveRoot(caller, target.ProjectID) {
			return nil
		}
		if coordownership.IsLiveCoordinator(caller, target.ProjectID) && target.OwnerSessionID != nil && *target.OwnerSessionID == caller.ID {
			return nil
		}
	}
	return apierr.New(apierr.KindForbidden, "CI_GATE_FORBIDDEN",
		"Only the project root or the owning coordinator may change a pull request's draft state",
		map[string]any{"sessionId": string(requestedBy)})
}

// SetReady marks the session's open PR ready for review or converts it back
// to a draft, then records who did it. An already-matching PR is a no-op.
func (s *ActionService) SetReady(ctx context.Context, request ReadyRequest) (ReadyResult, error) {
	store, ok := s.store.(readyStore)
	if !ok || s.draft == nil || s.reader == nil {
		return ReadyResult{}, fmt.Errorf("%w: no draft control is configured", ErrCIGateUnsupported)
	}
	target, found, err := store.GetSession(ctx, request.SessionID)
	if err != nil {
		return ReadyResult{}, fmt.Errorf("load session %s: %w", request.SessionID, err)
	}
	if !found {
		return ReadyResult{}, apierr.NotFound("SESSION_NOT_FOUND", "Unknown session")
	}
	if err := requireReadyAuthority(ctx, store, target, request.RequestedBySessionID); err != nil {
		return ReadyResult{}, err
	}
	prs, err := store.ListPRsBySession(ctx, request.SessionID)
	if err != nil {
		return ReadyResult{}, fmt.Errorf("list pull requests: %w", err)
	}
	var open []domain.PullRequest
	for _, pr := range prs {
		if !pr.Merged && !pr.Closed {
			open = append(open, pr)
		}
	}
	if len(open) == 0 {
		return ReadyResult{}, apierr.Conflict("NO_OPEN_PR", "The session has no open pull request",
			map[string]any{"sessionId": string(request.SessionID)})
	}
	if len(open) > 1 {
		return ReadyResult{}, apierr.Conflict("MULTIPLE_OPEN_PRS", "The session has several open pull requests",
			map[string]any{"sessionId": string(request.SessionID)})
	}
	tracked := open[0]
	repo, ok := scmRepoForPR(tracked)
	if !ok {
		return ReadyResult{}, fmt.Errorf("%w: pull request repository is unknown", ErrPRPreconditions)
	}
	repo.Account = s.accountForPR(ctx, tracked)
	ref := ports.SCMPRRef{Repo: repo, Number: tracked.Number, URL: tracked.URL}

	observations, err := s.reader.FetchPullRequests(ctx, []ports.SCMPRRef{ref})
	if err != nil {
		if errors.Is(err, ports.ErrSCMNotFound) {
			return ReadyResult{}, fmt.Errorf("%w: %w", ErrPRNotFound, err)
		}
		return ReadyResult{}, fmt.Errorf("refresh pull request: %w", err)
	}
	if len(observations) != 1 || !observations[0].Fetched || observations[0].PR.Number != ref.Number {
		return ReadyResult{}, ErrPRNotFound
	}
	live := observations[0].PR
	if live.Merged || live.Closed {
		return ReadyResult{}, apierr.Conflict("NO_OPEN_PR", "The session has no open pull request",
			map[string]any{"sessionId": string(request.SessionID)})
	}
	result := ReadyResult{PRURL: tracked.URL, PRNumber: tracked.Number, HeadSHA: live.HeadSHA, Ready: request.Ready}
	if live.Draft != request.Ready {
		return result, nil
	}
	if err := s.draft.SetPullRequestDraft(ctx, ports.SCMDraftRequest{PR: ref, Draft: !request.Ready}); err != nil {
		if errors.Is(err, ports.ErrSCMNotFound) {
			return ReadyResult{}, fmt.Errorf("%w: %w", ErrPRNotFound, err)
		}
		if errors.Is(err, ports.ErrSCMUnsupported) {
			return ReadyResult{}, fmt.Errorf("%w: %w", ErrCIGateUnsupported, err)
		}
		return ReadyResult{}, fmt.Errorf("set pull request draft state: %w", err)
	}
	result.Changed = true
	event := domain.SessionPRReadyEvent{
		SessionID:      request.SessionID,
		ActorSessionID: request.RequestedBySessionID,
		PRURL:          tracked.URL,
		HeadSHA:        strings.TrimSpace(live.HeadSHA),
		Ready:          request.Ready,
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.RecordSessionPRReadyEvent(ctx, event); err != nil {
		// The flip already happened; failing would make a retry a no-op.
		slog.Warn("pr ready: audit event not recorded", "session", request.SessionID, "pr", tracked.URL, "error", err)
	}
	return result, nil
}

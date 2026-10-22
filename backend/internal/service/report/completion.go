package report

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

// CompletionInput identifies one durable tick action and its merged PRs.
type CompletionInput struct {
	HostID    string               `json:"hostId"`
	SessionID string               `json:"sessionId"`
	ActionKey string               `json:"actionKey"`
	Kind      string               `json:"kind"`
	Recipient *CompletionRecipient `json:"recipient,omitempty"`
	PRs       []CompletionPR       `json:"prs"`
}

// CompletionPR is a provider merge fact included in a completion report.
type CompletionPR struct {
	Number         int    `json:"number"`
	MergeCommitSHA string `json:"mergeCommitSha"`
}

// DeliverCompletion resolves ownership locally and submits system context using
// the report delivery path. Retries use the same semantic acceptance key.
func (c *Coordinator) DeliverCompletion(ctx context.Context, in CompletionInput) error {
	if strings.TrimSpace(in.SessionID) == "" || strings.TrimSpace(in.ActionKey) == "" ||
		(in.Kind != "merge" && in.Kind != "close") || len(in.PRs) == 0 {
		return apierr.Invalid("INVALID_COMPLETION_REPORT", "Worker, action, kind and merged PRs are required", nil)
	}
	var owner *domain.SessionID
	var project domain.ProjectID
	if in.Recipient != nil {
		if in.Recipient.OwnerSessionID == "" {
			return nil
		}
		value := domain.SessionID(in.Recipient.OwnerSessionID)
		owner, project = &value, domain.ProjectID(in.Recipient.ProjectID)
	} else if in.HostID != "" {
		link, found, err := c.store.GetRemoteWorkerLink(ctx, in.HostID, domain.SessionID(in.SessionID))
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		owner, project = link.OwnerSessionID, link.ProjectID
	} else {
		worker, found, err := c.store.GetSession(ctx, domain.SessionID(in.SessionID))
		if err != nil {
			return err
		}
		if !found || worker.Kind != domain.KindWorker {
			return nil
		}
		owner, project = worker.OwnerSessionID, worker.ProjectID
	}
	if owner == nil {
		return nil
	}
	recipient, found, err := c.store.GetSession(ctx, *owner)
	if err != nil {
		return err
	}
	if !found || recipient.IsTerminated || recipient.Kind != domain.KindCoordinator ||
		recipient.ProjectID != project || recipient.Activity.State == domain.ActivityExited {
		return nil
	}
	// Validate every PR before claiming any, so a bad entry leaves no claim.
	for _, pr := range in.PRs {
		if pr.Number <= 0 || strings.TrimSpace(pr.MergeCommitSHA) == "" {
			return apierr.Invalid("INVALID_COMPLETION_REPORT", "PR number and merge commit are required", nil)
		}
	}
	var prs []string
	var claimed []string
	// A merge the observer already announced is not repeated here.
	for _, pr := range in.PRs {
		won, err := c.store.ClaimPRMergeAnnouncement(ctx, pr.MergeCommitSHA, "", domain.SessionID(in.SessionID), c.now().UTC())
		if err != nil {
			return errors.Join(err, c.releaseMergeClaims(ctx, claimed))
		}
		if !won {
			continue
		}
		claimed = append(claimed, pr.MergeCommitSHA)
		prs = append(prs, fmt.Sprintf("PR #%d merge commit %s", pr.Number, pr.MergeCommitSHA))
	}
	if len(prs) == 0 {
		return nil
	}
	message := fmt.Sprintf("TICK completion: worker %s/%s; %s; action %s completed.",
		in.HostID, in.SessionID, strings.Join(prs, "; "), in.Kind)
	identity := fmt.Sprintf("%q/%q/%q", in.HostID, in.SessionID, in.ActionKey)
	key := fmt.Sprintf("tick-completion:%x", sha256.Sum256([]byte(identity)))
	err = c.delivery.Submit(ctx, *owner, message, key)
	// A semantic timeout follows a written prompt; consume it like reports do.
	if errors.Is(err, domain.ErrSemanticAcceptanceTimeout) {
		return nil
	}
	if err != nil {
		return errors.Join(err, c.releaseMergeClaims(ctx, claimed))
	}
	return nil
}

// releaseMergeClaims drops claims whose message was not delivered so the
// next observation or tick announces those merges.
func (c *Coordinator) releaseMergeClaims(ctx context.Context, shas []string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	var errs []error
	for _, sha := range shas {
		if err := c.store.ReleasePRMergeAnnouncement(cleanupCtx, sha); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CompletionRecipient is a hub-owned snapshot taken before a completion action.
type CompletionRecipient struct {
	HostID         string `json:"hostId"`
	OwnerSessionID string `json:"ownerSessionId"`
	ProjectID      string `json:"projectId"`
}

package remotereports

import (
	"context"
	"errors"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

// checkHostLiveness reads each link's worker over the host's connection.
// It drops links that are gone or ended, and returns the links still used.
func (o *Observer) checkHostLiveness(ctx context.Context, client *hostClient, links []domain.RemoteWorkerLink) []domain.RemoteWorkerLink {
	var kept []domain.RemoteWorkerLink
	var retainedLinks []domain.RemoteWorkerLink
	keep := func(link domain.RemoteWorkerLink) {
		if retained(link) {
			retainedLinks = append(retainedLinks, link)
		} else {
			kept = append(kept, link)
		}
	}
	for i, link := range links {
		terminated, err := client.terminated(ctx, link.RemoteSessionID)
		switch {
		case errors.Is(err, errSessionGone):
			if o.drop(ctx, client, link, "remote session no longer exists") != nil {
				kept = append(kept, link)
			}
		case errors.Is(err, errCredentialRefused):
			o.refuse(link.HostID, client.password)
			return append(append(kept, retainedLinks...), links[i:]...)
		case err != nil:
			o.note("liveness "+link.HostID+"/"+string(link.RemoteSessionID), client.label, err)
			if !retained(link) || o.retainedPullDue(link) {
				keep(link)
			}
		case !terminated:
			o.recordLiveness(ctx, link, domain.RemoteLinkLive)
			link.LastState = domain.RemoteLinkLive
			link.TerminatedAt = nil
			o.clearRetainedPull(link)
			keep(link)
		default:
			if link.TerminatedAt == nil {
				endedAt := o.now()
				link.TerminatedAt = &endedAt
			}
			link.LastState = domain.RemoteLinkTerminated
			o.recordLiveness(ctx, link, domain.RemoteLinkTerminated)
			if !retained(link) || o.retainedPullDue(link) {
				if !o.retentionOver(link) || !o.endedWithNothingPending(ctx, client, link) {
					keep(link)
				}
			}
		}
	}
	return append(kept, retainedLinks...)
}

// endedWithNothingPending drops a terminated worker's link once it has no
// report left to deliver, due or not. Otherwise the pull delivers them first.
func (o *Observer) endedWithNothingPending(ctx context.Context, client *hostClient, link domain.RemoteWorkerLink) bool {
	// Report contents follow the pull's transport policy; a blocked host
	// keeps its terminated link, which already does not count.
	if remotehost.UnattendedTransportAllowed(client.base, client.insecureOK) != nil {
		return false
	}
	// The expired count is not logged here, so no seen count is sent.
	result, err := client.pending(ctx, link.RemoteSessionID, 0)
	if errors.Is(err, errSessionGone) {
		return o.drop(ctx, client, link, "remote session no longer exists") == nil
	}
	if err != nil || len(result.reports) > 0 || len(result.rejected) > 0 || result.more {
		return false
	}
	dropped, err := o.dropEnded(ctx, client, link)
	return dropped && err == nil
}

// dropEnded drops an ended worker's link once its host stops refreshing the
// worker's PRs, so a later merge or close report is still pulled.
func (o *Observer) dropEnded(ctx context.Context, client *hostClient, link domain.RemoteWorkerLink) (bool, error) {
	if !o.retentionOver(link) {
		return false, nil
	}
	return true, o.drop(ctx, client, link, "remote session terminated")
}

func (o *Observer) retentionOver(link domain.RemoteWorkerLink) bool {
	return link.TerminatedAt != nil && o.now().Sub(*link.TerminatedAt) >= domain.TerminatedPRRefreshWindow
}

// retainedPullInterval spaces pulls of ended workers' links; they only wait
// for a late PR outcome, so live links go first and on every pass.
const retainedPullInterval = 5 * time.Minute

// retained reports a link whose worker an earlier pass already read as ended.
func retained(link domain.RemoteWorkerLink) bool {
	return link.LastState == domain.RemoteLinkTerminated && link.TerminatedAt != nil
}

// retainedPullDue keeps settled terminated links on a slower pull cadence
// while still checking host liveness every pass for a possible restoration.
func (o *Observer) retainedPullDue(link domain.RemoteWorkerLink) bool {
	ref := linkRef{hostID: link.HostID, session: link.RemoteSessionID}
	now := o.now()
	o.retainedMu.Lock()
	defer o.retainedMu.Unlock()
	if last, ok := o.lastRetainedPull[ref]; ok && now.Sub(last) < retainedPullInterval {
		return false
	}
	o.lastRetainedPull[ref] = now
	return true
}

func (o *Observer) clearRetainedPull(link domain.RemoteWorkerLink) {
	ref := linkRef{hostID: link.HostID, session: link.RemoteSessionID}
	o.retainedMu.Lock()
	delete(o.lastRetainedPull, ref)
	o.retainedMu.Unlock()
}

func (o *Observer) recordLiveness(ctx context.Context, link domain.RemoteWorkerLink, state string) {
	if err := o.store.UpdateRemoteWorkerLinkLiveness(ctx, link.HostID, link.RemoteSessionID, o.now(), state); err != nil {
		o.log.Error("remote reports: record link liveness failed", "host", link.HostID, "session", link.RemoteSessionID, "err", err)
	}
}

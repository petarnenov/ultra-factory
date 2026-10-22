package remotereports

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

// checkHostLiveness reads each link's worker over the host's connection.
// It drops links that are gone or ended, and returns the links still used.
func (o *Observer) checkHostLiveness(ctx context.Context, client *hostClient, links []domain.RemoteWorkerLink) []domain.RemoteWorkerLink {
	var kept []domain.RemoteWorkerLink
	for i, link := range links {
		terminated, err := client.terminated(ctx, link.RemoteSessionID)
		switch {
		case errors.Is(err, errSessionGone):
			if o.drop(ctx, client, link, "remote session no longer exists") != nil {
				kept = append(kept, link)
			}
		case errors.Is(err, errCredentialRefused):
			o.refuse(link.HostID, client.password)
			return append(kept, links[i:]...)
		case err != nil:
			o.note("liveness "+link.HostID+"/"+string(link.RemoteSessionID), client.label, err)
			kept = append(kept, link)
		case !terminated:
			o.recordLiveness(ctx, link, domain.RemoteLinkLive)
			kept = append(kept, link)
		default:
			o.recordLiveness(ctx, link, domain.RemoteLinkTerminated)
			if !o.endedWithNothingPending(ctx, client, link) {
				kept = append(kept, link)
			}
		}
	}
	return kept
}

// endedWithNothingPending drops a terminated worker's link once it has no
// report left to deliver, due or not. Otherwise the pull delivers them first.
func (o *Observer) endedWithNothingPending(ctx context.Context, client *hostClient, link domain.RemoteWorkerLink) bool {
	// Report contents follow the pull's transport policy; a blocked host
	// keeps its terminated link, which already does not count.
	if remotehost.UnattendedTransportAllowed(client.base, client.insecureOK) != nil {
		return false
	}
	result, err := client.pending(ctx, link.RemoteSessionID)
	if errors.Is(err, errSessionGone) {
		return o.drop(ctx, client, link, "remote session no longer exists") == nil
	}
	if err != nil || len(result.reports) > 0 || len(result.rejected) > 0 || result.more {
		return false
	}
	return o.drop(ctx, client, link, "remote session terminated") == nil
}

func (o *Observer) recordLiveness(ctx context.Context, link domain.RemoteWorkerLink, state string) {
	if err := o.store.UpdateRemoteWorkerLinkLiveness(ctx, link.HostID, link.RemoteSessionID, o.now(), state); err != nil {
		o.log.Error("remote reports: record link liveness failed", "host", link.HostID, "session", link.RemoteSessionID, "err", err)
	}
}

package ownership

import (
	"context"
	"sort"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

// HostRef is one paired remote host.
type HostRef struct {
	ID    string
	Label string
}

// HostDirectory lists the paired remote hosts and names this machine, so a
// placement rule never accepts the local daemon as a host.
type HostDirectory interface {
	PairedHosts(ctx context.Context) ([]HostRef, error)
	SelfHostID() string
}

// SavedHosts reads the saved-host file. Self returns this daemon's host ID and
// may be nil before the identity is resolved.
type SavedHosts struct {
	Self func() string
}

// PairedHosts returns the saved hosts that carry a paired identity.
func (h SavedHosts) PairedHosts(context.Context) ([]HostRef, error) {
	path, err := remotehost.FilePath()
	if err != nil {
		return nil, err
	}
	entries, err := remotehost.ReadEntries(path)
	if err != nil {
		return nil, err
	}
	hosts := make([]HostRef, 0, len(entries))
	for _, entry := range entries {
		if id := strings.TrimSpace(entry.HostID); id != "" {
			hosts = append(hosts, HostRef{ID: id, Label: strings.TrimSpace(entry.Label)})
		}
	}
	return hosts, nil
}

// SelfHostID returns this machine's host ID, or "" when unknown.
func (h SavedHosts) SelfHostID() string {
	if h.Self == nil {
		return ""
	}
	return h.Self()
}

// HostNotAllowedError reports a host outside the coordinator's slots or the
// project's allowed hosts. coordinator is empty when no coordinator exists yet.
func HostNotAllowedError(coordinator domain.SessionID, host string) error {
	details := map[string]any{"hostId": host}
	if coordinator != "" {
		details["coordinatorId"] = string(coordinator)
	}
	return apierr.Conflict("COORDINATOR_HOST_NOT_ALLOWED", "The host is not allowed for this coordinator's workers", details)
}

// HostSlotsFullError reports a host whose slots the coordinator has all used.
func HostSlotsFullError(coordinator domain.SessionID, host string, current, allowed int) error {
	return apierr.Conflict("COORDINATOR_HOST_SLOTS_FULL", "The coordinator has no free worker slot on this host",
		map[string]any{"coordinatorId": string(coordinator), "hostId": host, "current": current, "allowed": allowed})
}

// LocalWorkerRefusedError reports a coordinator worker on the local daemon.
func LocalWorkerRefusedError(coordinator domain.SessionID, project domain.ProjectID) error {
	return apierr.Conflict("COORDINATOR_LOCAL_WORKER_REFUSED", "Coordinator workers may not run on the local daemon; pass --host, or set coordinators.allowLocalWorkers",
		map[string]any{"coordinatorId": string(coordinator), "projectId": string(project)})
}

// HostSlotsInvalidError rejects an invalid hostSlots value. limit is the
// coordinator's workerLimit for a total above it and nil for a bad entry.
func HostSlotsInvalidError(message string, limit *int) error {
	details := map[string]any{"field": "hostSlots"}
	if limit != nil {
		details["max"] = *limit
	}
	return apierr.Invalid("COORDINATOR_WORKER_LIMIT_INVALID", message, details)
}

// ResolveHost maps a host ID or label to a paired host's ID. The local
// daemon, unknown and ambiguous names are COORDINATOR_HOST_NOT_ALLOWED.
func ResolveHost(hosts HostDirectory, paired []HostRef, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", HostSlotsInvalidError("hostSlots entries need a hostId", nil)
	}
	if self := hosts.SelfHostID(); self != "" && name == self {
		return "", HostNotAllowedError("", name)
	}
	for _, host := range paired {
		if host.ID == name {
			return host.ID, nil
		}
	}
	match := ""
	for _, host := range paired {
		if host.Label != "" && strings.EqualFold(host.Label, name) {
			if match != "" && match != host.ID {
				return "", HostNotAllowedError("", name)
			}
			match = host.ID
		}
	}
	if match == "" {
		return "", HostNotAllowedError("", name)
	}
	return match, nil
}

// NormalizeHostSlots validates host slots and returns the stored form:
// IDs only, one entry per host, sorted. The total may not exceed workerLimit.
func NormalizeHostSlots(
	ctx context.Context,
	hosts HostDirectory,
	in []domain.HostSlot,
	allowedHosts []string,
	workerLimit int,
) ([]domain.HostSlot, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if hosts == nil {
		return nil, HostSlotsInvalidError("host slots need the paired host directory", nil)
	}
	paired, err := hosts.PairedHosts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.HostSlot, 0, len(in))
	seen := make(map[string]bool, len(in))
	total := 0
	for _, entry := range in {
		if entry.Slots < 1 {
			return nil, HostSlotsInvalidError("each hostSlots entry needs slots of at least 1", nil)
		}
		id, err := ResolveHost(hosts, paired, entry.HostID)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, HostSlotsInvalidError("hostSlots names the same host twice", nil)
		}
		seen[id] = true
		if len(allowedHosts) > 0 && !containsHost(allowedHosts, id) {
			return nil, HostNotAllowedError("", id)
		}
		total += entry.Slots
		out = append(out, domain.HostSlot{HostID: id, Slots: entry.Slots})
	}
	if total > workerLimit {
		return nil, HostSlotsInvalidError("hostSlots add up to more than the coordinator's workerLimit", &workerLimit)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HostID < out[j].HostID })
	return out, nil
}

// SameHostSlots compares two normalized lists.
func SameHostSlots(a, b []domain.HostSlot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsHost(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

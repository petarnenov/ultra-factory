package remotereports

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/remoteworker"
)

// DefaultDiscoveryInterval is how often saved hosts are scanned for live
// workers that have no link yet.
const DefaultDiscoveryInterval = 60 * time.Second

// Linker stores a worker that discovery found on a saved host.
type Linker interface {
	LinkDiscovered(context.Context, domain.RemoteWorkerLink) (bool, error)
}

// Sync runs one discovery pass over every saved host, in parallel.
// Each host has its own budget, so an offline host cannot delay the rest.
func (o *Observer) Sync(ctx context.Context) ([]remoteworker.HostSync, error) {
	if o == nil || o.linker == nil || o.entries == nil {
		return nil, errors.New("remote discovery: linker and saved hosts are required")
	}
	o.discoverMu.Lock()
	defer o.discoverMu.Unlock()
	entries, err := o.entries()
	if err != nil {
		return nil, err
	}
	o.lastDiscovery = o.now()
	o.savedHosts = savedHostsSignature(entries)
	results := o.discoverAll(ctx, entries)
	linked := 0
	for _, result := range results {
		linked += result.Linked
	}
	if linked > 0 {
		o.Wake()
	}
	return results, ctx.Err()
}

// discoverIfDue runs a pass when the interval has elapsed or the saved hosts
// changed, so a host added or re-paired is scanned without waiting.
func (o *Observer) discoverIfDue(ctx context.Context) {
	if o.linker == nil || o.entries == nil {
		return
	}
	entries, err := o.entries()
	if err != nil {
		o.note("remote discovery", "remotes.json", err)
		return
	}
	signature := savedHostsSignature(entries)
	o.discoverMu.Lock()
	due := o.lastDiscovery.IsZero() || o.now().Sub(o.lastDiscovery) >= o.discoveryInterval || signature != o.savedHosts
	o.discoverMu.Unlock()
	if !due {
		return
	}
	if _, err := o.Sync(ctx); err != nil && !errors.Is(err, context.Canceled) {
		o.log.Warn("remote discovery: pass failed", "err", err)
	}
}

// discoverAll scans each distinct saved host, skipping this machine.
func (o *Observer) discoverAll(ctx context.Context, entries []remotehost.Entry) []remoteworker.HostSync {
	seen := map[string]bool{}
	var hosts []remotehost.Entry
	for _, entry := range entries {
		hostID := strings.TrimSpace(entry.HostID)
		if hostID == "" || seen[hostID] || (o.selfHostID != "" && hostID == o.selfHostID) {
			continue
		}
		seen[hostID] = true
		entry.HostID = hostID
		hosts = append(hosts, entry)
	}
	results := make([]remoteworker.HostSync, len(hosts))
	var wg sync.WaitGroup
	for i, entry := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = o.discoverHost(ctx, entry)
		}()
	}
	wg.Wait()
	return results
}

// discoverHost links the live workers of one saved host that have no link.
// A blocked or refused host is not contacted.
func (o *Observer) discoverHost(ctx context.Context, entry remotehost.Entry) remoteworker.HostSync {
	label := strings.TrimSpace(entry.Label)
	if label == "" {
		label = entry.HostID
	}
	result := remoteworker.HostSync{HostID: entry.HostID, Label: label}
	hostCtx, cancel := context.WithTimeout(ctx, o.hostBudget)
	defer cancel()
	client, err := o.connect(hostCtx, entry.HostID)
	if err != nil {
		if base, baseErr := remotehost.BaseURL(entry.URL); baseErr == nil && remotehost.UnattendedTransportAllowed(base, entry.AllowInsecureHTTP) != nil {
			result.Blocked = err.Error()
		} else {
			result.Error = err.Error()
		}
		o.note("discover "+entry.HostID, label, err)
		return result
	}
	defer client.close()
	sessions, err := client.sessions(hostCtx)
	if errors.Is(err, errCredentialRefused) {
		o.refuse(entry.HostID, client.password)
	}
	if err != nil {
		result.Error = err.Error()
		o.note("discover "+entry.HostID, label, err)
		return result
	}
	coordinated, live := remoteCoordination(sessions)
	removed, err := o.removedWorkers(ctx, entry.HostID)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	for _, session := range sessions {
		if session.Kind != string(domain.KindWorker) || session.IsTerminated {
			continue
		}
		// The host's own coordinator drives this worker; linking it would
		// let this daemon pull and claim reports the host still needs.
		if coordinated[session.ProjectID] || (session.OwnerSessionID != nil && live[*session.OwnerSessionID]) {
			result.Skipped++
			continue
		}
		if removed[session.ID] && !o.relinkRemoved(hostCtx, client, entry.HostID, session.ID) {
			result.Skipped++
			continue
		}
		created, err := o.linker.LinkDiscovered(ctx, domain.RemoteWorkerLink{
			HostID: entry.HostID, RemoteSessionID: session.ID,
			RemoteProjectID: domain.ProjectID(session.ProjectID), ProjectID: domain.ProjectID(session.ProjectID),
			DisplayName: session.DisplayName,
		})
		if errors.Is(err, remoteworker.ErrLinkLimit) {
			result.Error = err.Error()
			o.log.Warn("remote discovery: link limit reached; not linking more workers", "host", label, "limit", remoteworker.MaxLinks)
			break
		}
		switch {
		case err != nil && !errors.Is(err, remoteworker.ErrProjectNotRegistered) && !errors.Is(err, domain.ErrInvalidRemoteWorkerLink):
			result.Skipped++
			o.note(fmt.Sprintf("discover link %s/%s", entry.HostID, session.ID), label, err)
		case created:
			result.Linked++
		default:
			// Already linked, or a project this daemon does not have.
			result.Skipped++
		}
	}
	o.note("discover "+entry.HostID, label, nil)
	return result
}

// removedWorkers returns the sessions of one host an operator dropped by hand.
func (o *Observer) removedWorkers(ctx context.Context, hostID string) (map[domain.SessionID]bool, error) {
	removals, err := o.store.ListRemoteWorkerLinkRemovals(ctx)
	if err != nil {
		return nil, err
	}
	removed := map[domain.SessionID]bool{}
	for _, removal := range removals {
		if removal.HostID == hostID {
			removed[removal.RemoteSessionID] = true
		}
	}
	return removed, nil
}

// relinkRemoved reports whether a removed worker may be linked again. It may
// not while the host reports it terminated; once it is live again the
// removal is forgotten and discovery links it as usual.
func (o *Observer) relinkRemoved(ctx context.Context, client *hostClient, hostID string, sessionID domain.SessionID) bool {
	terminated, err := client.terminated(ctx, sessionID)
	if err != nil || terminated {
		return false
	}
	if err := o.store.DeleteRemoteWorkerLinkRemoval(ctx, hostID, sessionID); err != nil {
		o.log.Error("remote discovery: forget link removal failed", "host", hostID, "session", sessionID, "err", err)
		return false
	}
	return true
}

// remoteCoordination returns the projects with a live orchestrator or
// coordinator on a host, and the IDs of its live sessions.
func remoteCoordination(sessions []wireSession) (map[string]bool, map[string]bool) {
	coordinated, live := map[string]bool{}, map[string]bool{}
	for _, session := range sessions {
		if session.IsTerminated {
			continue
		}
		live[string(session.ID)] = true
		if session.Kind == string(domain.KindOrchestrator) || session.Kind == string(domain.KindCoordinator) {
			coordinated[session.ProjectID] = true
		}
	}
	return coordinated, live
}

// savedHostsSignature identifies the saved hosts a pass scanned. It hashes the
// password so a re-pasted credential is noticed without keeping it.
func savedHostsSignature(entries []remotehost.Entry) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, strings.Join([]string{entry.HostID, entry.URL, entry.Label, fingerprint(entry.Password)}, "\x00"))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

// readSavedHosts reads the saved-host file that the daemon shares with the CLI.
func readSavedHosts() ([]remotehost.Entry, error) {
	path, err := remotehost.FilePath()
	if err != nil {
		return nil, err
	}
	return remotehost.ReadEntries(path)
}

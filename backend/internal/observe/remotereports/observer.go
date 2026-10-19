// Package remotereports pulls reports from workers that run on saved remote AO
// hosts and delivers them to this daemon's orchestrators. A remote worker's
// `ao report` lands in its own host's outbox, where no orchestrator for the
// project runs; this observer is the only path from there to the orchestrator
// that spawned it. All traffic is outbound to saved, identity-checked hosts.
package remotereports

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
	reportsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/report"
)

// DefaultInterval is how often linked hosts are polled when nothing wakes the
// observer sooner.
const DefaultInterval = 15 * time.Second

// defaultRequestTimeout bounds each authenticated request to a remote host.
const defaultRequestTimeout = 10 * time.Second

// errHostBudget reports a host that used up its budget for this pass.
var errHostBudget = errors.New("host is too slow")

// defaultHostBudget bounds one pass over one host's links.
const defaultHostBudget = 45 * time.Second

// Store is the link and delivery-history boundary.
type Store interface {
	ListRemoteWorkerLinks(context.Context) ([]domain.RemoteWorkerLink, error)
	DeleteRemoteWorkerLink(context.Context, string, domain.SessionID) error
	ListDeliveredRemoteReports(context.Context, string, domain.SessionID) (map[string]bool, error)
	RecordDeliveredRemoteReports(context.Context, string, domain.SessionID, []string, time.Time) error
	RecordRemoteReportHistory(context.Context, string, domain.SessionID, []domain.ReportRecord) error
}

// Deliverer submits pulled reports to a project's active orchestrator.
type Deliverer interface {
	DeliverExternal(context.Context, domain.ProjectID, []domain.ReportRecord) error
}

// Deps configures an Observer. Lookup defaults to the saved-host file,
// HTTPClient to http.DefaultClient's transport, Interval to DefaultInterval.
type Deps struct {
	Store      Store
	Deliverer  Deliverer
	SelfHostID string
	Lookup     func(hostID string) (remotehost.Entry, bool, error)
	HTTPClient *http.Client
	Interval   time.Duration
	// HostBudget bounds one pass over one host's pulls, and separately its
	// acknowledgements; defaults to 45 s.
	HostBudget time.Duration
	// RequestTimeout bounds each request to a remote host; defaults to 10 s.
	RequestTimeout time.Duration
	Now            func() time.Time
	Logger         *slog.Logger
}

// Observer polls linked remote workers for reports.
type Observer struct {
	store      Store
	deliverer  Deliverer
	selfHostID string
	lookup     func(string) (remotehost.Entry, bool, error)
	httpClient *http.Client
	interval   time.Duration
	hostBudget time.Duration
	reqTimeout time.Duration
	now        func() time.Time
	log        *slog.Logger
	wake       chan struct{}

	mu       sync.Mutex
	failures map[string]string
	refused  map[string]string
}

// New constructs an idle Observer.
func New(d Deps) *Observer {
	if d.Lookup == nil {
		d.Lookup = remotehost.Find
	}
	if d.Interval <= 0 {
		d.Interval = DefaultInterval
	}
	if d.HostBudget <= 0 {
		d.HostBudget = defaultHostBudget
	}
	if d.RequestTimeout <= 0 {
		d.RequestTimeout = defaultRequestTimeout
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &Observer{
		store: d.Store, deliverer: d.Deliverer, selfHostID: d.SelfHostID, lookup: d.Lookup,
		httpClient: d.HTTPClient, interval: d.Interval, hostBudget: d.HostBudget, reqTimeout: d.RequestTimeout, now: d.Now, log: d.Logger,
		wake: make(chan struct{}, 1), failures: map[string]string{}, refused: map[string]string{},
	}
}

// Wake asks the observer to poll now, e.g. right after a new link.
func (o *Observer) Wake() {
	if o == nil {
		return
	}
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

// Start polls until ctx is cancelled and returns its completion signal.
func (o *Observer) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(o.interval)
		defer ticker.Stop()
		for {
			if err := o.Poll(ctx); err != nil && !errors.Is(err, context.Canceled) {
				o.log.Warn("remote reports: poll failed", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-o.wake:
			}
		}
	}()
	return done
}

// settlePass is what one pass knows about each host while settling. A host
// that ran out of its pull budget, failed an acknowledgement, or used up its
// acknowledgement budget is unreachable: its remaining acknowledgements wait
// for the next pass instead of holding up every project settled after it.
type settlePass struct {
	unreachable map[string]bool
	deadlines   map[string]time.Time
}

func newSettlePass() *settlePass {
	return &settlePass{unreachable: map[string]bool{}, deadlines: map[string]time.Time{}}
}

// ackDeadline starts a host's acknowledgement budget at its first
// acknowledgement of the pass, across every project.
func (p *settlePass) ackDeadline(hostID string, budget time.Duration) time.Time {
	deadline, ok := p.deadlines[hostID]
	if !ok {
		deadline = time.Now().Add(budget)
		p.deadlines[hostID] = deadline
	}
	return deadline
}

// pulled is one link's due reports from one poll.
type pulled struct {
	link     domain.RemoteWorkerLink
	host     *hostClient
	fresh    []domain.ReportRecord
	settleID []string // every pulled ID to acknowledge: fresh, already delivered, rejected
}

// Poll performs one pass over every link. A host that cannot be reached,
// verified, or authenticated is skipped without affecting other hosts; a
// project with no active orchestrator keeps its reports on the remote host
// for the next pass.
func (o *Observer) Poll(ctx context.Context) error {
	if o == nil || o.store == nil || o.deliverer == nil {
		return errors.New("remote reports: dependencies are required")
	}
	links, err := o.store.ListRemoteWorkerLinks(ctx)
	if err != nil {
		return err
	}
	byHost := map[string][]domain.RemoteWorkerLink{}
	var hosts []string
	for _, link := range links {
		if _, seen := byHost[link.HostID]; !seen {
			hosts = append(hosts, link.HostID)
		}
		byHost[link.HostID] = append(byHost[link.HostID], link)
	}
	byProject := map[domain.ProjectID][]*pulled{}
	var projects []domain.ProjectID
	var clients []*hostClient
	pass := newSettlePass()
	// Clients stay open through settle, so each acknowledgement reuses the
	// connection that answered the identity probe.
	defer func() {
		for _, client := range clients {
			client.close()
		}
	}()
	for _, hostID := range hosts {
		client, items, err := o.pollHost(ctx, hostID, byHost[hostID])
		if client != nil {
			clients = append(clients, client)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		o.note("host "+hostID, hostID, err)
		if errors.Is(err, errHostBudget) {
			pass.unreachable[hostID] = true
		}
		if errors.Is(err, errCredentialRefused) {
			// Never send a password the host just refused, not even to settle.
			continue
		}
		for _, item := range items {
			if _, seen := byProject[item.link.ProjectID]; !seen {
				projects = append(projects, item.link.ProjectID)
			}
			byProject[item.link.ProjectID] = append(byProject[item.link.ProjectID], item)
		}
	}
	for _, projectID := range projects {
		for _, item := range byProject[projectID] {
			if len(item.fresh) > 0 {
				if err := o.store.RecordRemoteReportHistory(ctx, item.link.HostID, item.link.RemoteSessionID, item.fresh); err != nil {
					o.log.Error("remote reports: record report history failed", "host", item.host.label, "session", item.link.RemoteSessionID, "err", err)
				}
			}
		}
		o.settle(ctx, projectID, byProject[projectID], pass)
	}
	return ctx.Err()
}

// pollHost pulls every link of one host within hostBudget, so a slow host
// cannot hold up the others for long. A refused credential stops the host.
// The caller closes the returned client after settling.
func (o *Observer) pollHost(ctx context.Context, hostID string, links []domain.RemoteWorkerLink) (*hostClient, []*pulled, error) {
	hostCtx, cancel := context.WithTimeout(ctx, o.hostBudget)
	defer cancel()
	client, err := o.connect(hostCtx, hostID)
	if err != nil {
		return nil, nil, err
	}
	var items []*pulled
	for _, link := range links {
		item, err := o.pull(hostCtx, client, link)
		if errors.Is(err, errCredentialRefused) {
			o.refuse(hostID, client.password)
			return client, nil, err
		}
		if hostCtx.Err() != nil && ctx.Err() == nil {
			return client, items, fmt.Errorf("%w: over %s; the rest of its links wait for the next pass", errHostBudget, o.hostBudget)
		}
		o.note("link "+hostID+"/"+string(link.RemoteSessionID), client.label, err)
		if err != nil || item == nil {
			continue
		}
		items = append(items, item)
	}
	return client, items, nil
}

// pull reads one link's due reports. It returns nil when there is nothing to
// deliver or acknowledge, and drops the link only once the remote worker is
// gone and has nothing pending at all, due or not.
func (o *Observer) pull(ctx context.Context, client *hostClient, link domain.RemoteWorkerLink) (*pulled, error) {
	result, err := client.pending(ctx, link.RemoteSessionID)
	if errors.Is(err, errSessionGone) {
		return nil, o.drop(ctx, client, link, "remote session no longer exists")
	}
	if err != nil {
		return nil, err
	}
	if len(result.reports) == 0 && len(result.rejected) == 0 {
		if result.more {
			return nil, nil
		}
		terminated, err := client.terminated(ctx, link.RemoteSessionID)
		if errors.Is(err, errSessionGone) {
			return nil, o.drop(ctx, client, link, "remote session no longer exists")
		}
		if err != nil || !terminated {
			return nil, err
		}
		// The worker may have reported between the two reads; drop the link
		// only if it still has nothing pending now that it has ended.
		again, err := client.pending(ctx, link.RemoteSessionID)
		if err != nil && !errors.Is(err, errSessionGone) {
			return nil, err
		}
		if err == nil && (len(again.reports) > 0 || len(again.rejected) > 0 || again.more) {
			return nil, nil
		}
		return nil, o.drop(ctx, client, link, "remote session terminated")
	}
	delivered, err := o.store.ListDeliveredRemoteReports(ctx, link.HostID, link.RemoteSessionID)
	if err != nil {
		return nil, err
	}
	item := &pulled{link: link, host: client}
	for _, id := range result.rejected {
		o.log.Warn("remote reports: dropping a report this daemon cannot deliver", "host", client.label, "session", link.RemoteSessionID, "report", id)
		item.settleID = append(item.settleID, id)
	}
	for _, report := range result.reports {
		item.settleID = append(item.settleID, report.ID)
		if delivered[report.ID] {
			continue
		}
		report.SessionID = link.RemoteSessionID
		if report.SourceProjectID == "" {
			report.SourceProjectID = string(report.ProjectID)
		}
		report.ProjectID = link.ProjectID
		report.SessionDisplayName = link.DisplayName
		report.HostLabel = client.label
		report.HostID = link.HostID
		item.fresh = append(item.fresh, report)
	}
	return item, nil
}

// settle delivers a project's fresh reports in one turn, records them, then
// acknowledges every pulled report on its host. A failed delivery leaves the
// remote reports pending. The local record only matters if acknowledgement
// fails: it keeps the next pass from delivering the same report again.
func (o *Observer) settle(ctx context.Context, projectID domain.ProjectID, items []*pulled, pass *settlePass) {
	var fresh []domain.ReportRecord
	for _, item := range items {
		fresh = append(fresh, item.fresh...)
	}
	sort.SliceStable(fresh, func(i, j int) bool { return fresh[i].CreatedAt.Before(fresh[j].CreatedAt) })
	if len(fresh) > 0 {
		err := o.deliverer.DeliverExternal(ctx, projectID, fresh)
		batch := reportsvc.PreparedBatch{ProjectID: projectID, Reports: fresh}
		unconfirmed := errors.Is(err, domain.ErrSemanticAcceptanceTimeout)
		switch {
		case err == nil:
		case unconfirmed:
			o.log.Warn("remote report delivery unconfirmed; batch settled", "delivery_id", batch.IdempotencyKey(), "err", err)
		default:
			o.log.Warn("remote report delivery failed", "delivery_id", batch.IdempotencyKey(), "err", err)
		}
		o.note("project "+string(projectID), string(projectID), err)
		// The prompt was written before an acceptance timeout, so the batch
		// is settled: re-pulling it would deliver the same prompt again.
		if err != nil && !unconfirmed {
			return
		}
	}
	at := o.now().UTC()
	for _, item := range items {
		ids := make([]string, len(item.fresh))
		for i, report := range item.fresh {
			ids[i] = report.ID
		}
		if err := o.store.RecordDeliveredRemoteReports(ctx, item.link.HostID, item.link.RemoteSessionID, ids, at); err != nil {
			o.log.Error("remote reports: record delivery failed", "host", item.host.label, "session", item.link.RemoteSessionID, "err", err)
		}
		hostID := item.link.HostID
		deadline := pass.ackDeadline(hostID, o.hostBudget)
		if pass.unreachable[hostID] || !time.Now().Before(deadline) || o.refusedPassword(hostID, item.host.password) {
			// The next pass acknowledges these again; the local record keeps
			// them from being delivered twice.
			pass.unreachable[hostID] = true
			continue
		}
		ackCtx, cancel := context.WithDeadline(ctx, deadline)
		err := item.host.acknowledge(ackCtx, item.link.RemoteSessionID, item.settleID)
		cancel()
		if err != nil {
			pass.unreachable[hostID] = true
		}
		if errors.Is(err, errCredentialRefused) {
			o.refuse(item.link.HostID, item.host.password)
		}
		o.note("ack "+item.link.HostID+"/"+string(item.link.RemoteSessionID), item.host.label, err)
	}
}

func (o *Observer) drop(ctx context.Context, client *hostClient, link domain.RemoteWorkerLink, reason string) error {
	if err := o.store.DeleteRemoteWorkerLink(ctx, link.HostID, link.RemoteSessionID); err != nil {
		return err
	}
	o.log.Info("remote reports: link removed", "host", client.label, "session", link.RemoteSessionID, "reason", reason)
	return nil
}

// note logs a failure once per distinct error for key and its recovery once,
// so a lasting problem does not repeat the same line every pass.
func (o *Observer) note(key, subject string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	previous, failing := o.failures[key]
	if err == nil {
		if failing {
			delete(o.failures, key)
			o.log.Info("remote reports: recovered", "what", key)
		}
		return
	}
	if previous != err.Error() {
		o.failures[key] = err.Error()
		level := slog.LevelWarn
		if errors.Is(err, reportsvc.ErrNoActiveOrchestrator) {
			level = slog.LevelInfo
		}
		o.log.Log(context.Background(), level, "remote reports: skipped", "what", key, "subject", subject, "err", err)
	}
}

// refuse stops sending a password the host rejected until the saved entry
// changes, so a rotated password cannot trip the host's lockout every pass.
func (o *Observer) refuse(hostID, password string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.refused[hostID] = fingerprint(password)
}

func (o *Observer) refusedPassword(hostID, password string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	refused, ok := o.refused[hostID]
	if ok && refused != fingerprint(password) {
		delete(o.refused, hostID)
		return false
	}
	return ok
}

func fingerprint(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// connect resolves a saved host and proves its identity before any request
// that carries the saved credential. The probe and the authenticated requests
// share one client that stays open until the pass has settled, so the
// credential goes over the connection that answered the probe whenever the
// host keeps it open.
func (o *Observer) connect(ctx context.Context, hostID string) (*hostClient, error) {
	if o.selfHostID != "" && hostID == o.selfHostID {
		return nil, errors.New("linked host is this machine")
	}
	entry, found, err := o.lookup(hostID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("no saved remote host has this host id")
	}
	base, err := remotehost.BaseURL(entry.URL)
	if err != nil {
		return nil, err
	}
	if err := remotehost.UnattendedTransportAllowed(base); err != nil {
		return nil, err
	}
	if o.refusedPassword(hostID, entry.Password) {
		return nil, errCredentialRefused
	}
	// Only the probe may open a connection for this pass; see ProbedClient.
	client, gate := remotehost.ProbedClient(o.httpClient, o.reqTimeout)
	actual, err := remotehost.ReadIdentity(ctx, client, base)
	gate.Close()
	if err != nil {
		closeIdle(client)
		return nil, err
	}
	if actual != hostID {
		closeIdle(client)
		return nil, errors.New("remote host identity changed; re-pair it in the desktop app")
	}
	label := entry.Label
	if label == "" {
		label = hostID
	}
	return &hostClient{http: client, base: base, hostID: hostID, password: entry.Password, label: label}, nil
}

func (h *hostClient) close() { closeIdle(h.http) }

func closeIdle(client http.Client) {
	if transport, ok := client.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

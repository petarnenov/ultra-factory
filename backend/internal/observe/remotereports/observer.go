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
	UpdateRemoteWorkerLinkLiveness(context.Context, string, domain.SessionID, time.Time, string) error
	ListRemoteWorkerLinkRemovals(context.Context) ([]domain.RemoteWorkerLinkRemoval, error)
	DeleteRemoteWorkerLinkRemoval(context.Context, string, domain.SessionID) error
}

// Deliverer resolves a link's durable owner and submits one owner group.
type Deliverer interface {
	ResolveRemoteRecipient(context.Context, domain.RemoteWorkerLink) (domain.SessionRecord, bool, error)
	DeliverExternal(context.Context, domain.ProjectID, domain.SessionID, []domain.ReportRecord) error
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
	// Linker stores workers that discovery finds; nil disables discovery.
	Linker Linker
	// Entries reads the saved hosts; it defaults to the remotes.json file.
	Entries func() ([]remotehost.Entry, error)
	// DiscoveryInterval is how often saved hosts are scanned for live
	// workers; it defaults to DefaultDiscoveryInterval.
	DiscoveryInterval time.Duration
}

// Observer polls linked remote workers for reports.
type Observer struct {
	store             Store
	deliverer         Deliverer
	selfHostID        string
	lookup            func(string) (remotehost.Entry, bool, error)
	httpClient        *http.Client
	interval          time.Duration
	hostBudget        time.Duration
	reqTimeout        time.Duration
	now               func() time.Time
	log               *slog.Logger
	wake              chan struct{}
	linker            Linker
	entries           func() ([]remotehost.Entry, error)
	discoveryInterval time.Duration

	// retainedMu guards when each ended worker's link was last pulled.
	retainedMu       sync.Mutex
	lastRetainedPull map[linkRef]time.Time

	// discoverMu serialises discovery passes and guards their state.
	discoverMu    sync.Mutex
	lastDiscovery time.Time
	savedHosts    string

	mu       sync.Mutex
	failures map[string]string
	refused  map[string]string
	// legacyWarned: hosts already warned as predating per-puller acks.
	legacyWarned map[string]bool
	// expiredSeen: the expired total last logged per link, sent back as
	// expiredSeen so the host reports only what came after it.
	expiredSeen map[linkRef]int
	// unrecorded: delivered IDs per link whose record write failed;
	// every pass retries the write until it succeeds.
	unrecorded map[linkRef]map[string]bool
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
	if d.Entries == nil {
		d.Entries = readSavedHosts
	}
	if d.DiscoveryInterval <= 0 {
		d.DiscoveryInterval = DefaultDiscoveryInterval
	}
	return &Observer{
		store: d.Store, deliverer: d.Deliverer, selfHostID: d.SelfHostID, lookup: d.Lookup,
		httpClient: d.HTTPClient, interval: d.Interval, hostBudget: d.HostBudget, reqTimeout: d.RequestTimeout, now: d.Now, log: d.Logger,
		linker: d.Linker, entries: d.Entries, discoveryInterval: d.DiscoveryInterval,
		wake: make(chan struct{}, 1), failures: map[string]string{}, refused: map[string]string{}, unrecorded: map[linkRef]map[string]bool{},
		legacyWarned: map[string]bool{}, expiredSeen: map[linkRef]int{}, lastRetainedPull: map[linkRef]time.Time{},
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
	if remotehost.AllowHTTPEnvSet() {
		o.log.Warn(remotehost.AllowHTTPEnvDeprecation)
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(o.interval)
		defer ticker.Stop()
		for {
			o.discoverIfDue(ctx)
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
	o.flushUnrecorded(ctx)
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
		// One connection per host serves the liveness read and the pulls, so
		// the credential only ever goes over the connection that answered.
		client, links, err := o.liveHost(ctx, hostID, o.schedule(hostID, byHost[hostID]))
		var items []*pulled
		if client != nil {
			clients = append(clients, client)
		}
		if err == nil {
			items, err = o.pollHost(ctx, client, links)
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

// liveHost dials one host for the pass and reads its links' liveness. The
// returned links are the ones still in use. A dial error keeps every link.
func (o *Observer) liveHost(ctx context.Context, hostID string, links []domain.RemoteWorkerLink) (*hostClient, []domain.RemoteWorkerLink, error) {
	hostCtx, cancel := context.WithTimeout(ctx, o.hostBudget)
	defer cancel()
	client, err := o.connect(hostCtx, hostID)
	if err != nil {
		return nil, links, err
	}
	return client, o.checkHostLiveness(hostCtx, client, links), nil
}

// pollHost pulls every link of one host within hostBudget; a refused
// credential stops the host. The caller closes the client after settling.
func (o *Observer) pollHost(ctx context.Context, client *hostClient, links []domain.RemoteWorkerLink) ([]*pulled, error) {
	hostID := client.hostID
	if o.refusedPassword(hostID, client.password) {
		return nil, errCredentialRefused
	}
	hostCtx, cancel := context.WithTimeout(ctx, o.hostBudget)
	defer cancel()
	var items []*pulled
	for _, link := range links {
		item, err := o.pull(hostCtx, client, link)
		if errors.Is(err, errCredentialRefused) {
			o.refuse(hostID, client.password)
			return nil, err
		}
		if hostCtx.Err() != nil && ctx.Err() == nil {
			return items, fmt.Errorf("%w: over %s; the rest of its links wait for the next pass", errHostBudget, o.hostBudget)
		}
		o.note("link "+hostID+"/"+string(link.RemoteSessionID), client.label, err)
		if err != nil || item == nil {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// pull reads one link's due reports. It returns nil when there is nothing to
// deliver or acknowledge, and drops the link only once the remote worker is
// gone and has nothing pending at all, due or not.
func (o *Observer) pull(ctx context.Context, client *hostClient, link domain.RemoteWorkerLink) (*pulled, error) {
	ref := linkRef{hostID: client.hostID, session: link.RemoteSessionID}
	result, err := client.pending(ctx, link.RemoteSessionID, o.expiredSeenFor(ref))
	if errors.Is(err, errSessionGone) {
		return nil, o.drop(ctx, client, link, "remote session no longer exists")
	}
	if err != nil {
		return nil, err
	}
	o.noteExpired(ref, client.label, result.unseen, result.total)
	o.noteLegacyHost(client)
	if len(result.reports) == 0 && len(result.rejected) == 0 {
		if result.more || (retained(link) && !o.retentionOver(link)) {
			return nil, nil
		}
		terminated, err := client.terminated(ctx, link.RemoteSessionID)
		if errors.Is(err, errSessionGone) {
			return nil, o.drop(ctx, client, link, "remote session no longer exists")
		}
		if err != nil || !terminated {
			return nil, err
		}
		// The worker may have reported since the first read: drop the link
		// only if nothing is pending now. The seen count was already sent.
		again, err := client.pending(ctx, link.RemoteSessionID, 0)
		if err != nil && !errors.Is(err, errSessionGone) {
			return nil, err
		}
		if err == nil && (len(again.reports) > 0 || len(again.rejected) > 0 || again.more) {
			return nil, nil
		}
		_, err = o.dropEnded(ctx, client, link)
		return nil, err
	}
	delivered, err := o.store.ListDeliveredRemoteReports(ctx, link.HostID, link.RemoteSessionID)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	for id := range o.unrecorded[linkRef{link.HostID, link.RemoteSessionID}] {
		delivered[id] = true
	}
	o.mu.Unlock()
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

// settle delivers each owner group, then acknowledges each link after its
// group was accepted; a failed group stays pending on its remote host.
func (o *Observer) settle(ctx context.Context, projectID domain.ProjectID, items []*pulled, pass *settlePass) {
	groups := map[domain.SessionID][]*pulled{}
	var ackOnly []*pulled
	for _, item := range items {
		if len(item.fresh) == 0 {
			ackOnly = append(ackOnly, item)
			continue
		}
		recipient, ok, err := o.deliverer.ResolveRemoteRecipient(ctx, item.link)
		if err != nil {
			o.note("link "+item.link.HostID+"/"+string(item.link.RemoteSessionID), item.host.label, err)
			continue
		}
		if !ok {
			o.note("project "+string(projectID), string(projectID), fmt.Errorf("%w: %s", reportsvc.ErrNoActiveOrchestrator, projectID))
			continue
		}
		groups[recipient.ID] = append(groups[recipient.ID], item)
	}
	recipients := make([]domain.SessionID, 0, len(groups))
	for recipient := range groups {
		recipients = append(recipients, recipient)
	}
	sort.Slice(recipients, func(i, j int) bool { return recipients[i] < recipients[j] })
	for _, recipient := range recipients {
		group := groups[recipient]
		var fresh []domain.ReportRecord
		for _, item := range group {
			fresh = append(fresh, item.fresh...)
		}
		sort.SliceStable(fresh, func(i, j int) bool { return fresh[i].CreatedAt.Before(fresh[j].CreatedAt) })
		err := o.deliverer.DeliverExternal(ctx, projectID, recipient, fresh)
		batch := reportsvc.PreparedBatch{ProjectID: projectID, Reports: fresh}
		unconfirmed := errors.Is(err, domain.ErrSemanticAcceptanceTimeout)
		switch {
		case err == nil:
		case unconfirmed:
			o.log.Warn("remote report delivery unconfirmed; batch settled", "delivery_id", batch.IdempotencyKey(), "recipient", recipient, "err", err)
		default:
			o.log.Warn("remote report delivery failed", "delivery_id", batch.IdempotencyKey(), "recipient", recipient, "err", err)
		}
		o.note("group "+string(projectID)+"/"+string(recipient), string(projectID), err)
		// The prompt was written before an acceptance timeout, so the group is
		// settled: re-pulling it would deliver the same prompt again.
		if err != nil && !unconfirmed {
			continue
		}
		o.acknowledge(ctx, group, pass)
	}
	// Rejections and reports already recorded as delivered still need their
	// acknowledgement even when their group had nothing fresh to deliver.
	o.acknowledge(ctx, ackOnly, pass)
}

// acknowledge records one delivered group per link, then acknowledges each
// link separately; an unreachable host keeps its reports pending.
func (o *Observer) acknowledge(ctx context.Context, items []*pulled, pass *settlePass) {
	at := o.now().UTC()
	for _, item := range items {
		key := linkRef{item.link.HostID, item.link.RemoteSessionID}
		// Retry failed writes too; until one lands, pull skips these IDs
		// via the in-memory set, even if the host ack fails.
		o.mu.Lock()
		ids := make([]string, 0, len(item.fresh)+len(o.unrecorded[key]))
		for _, report := range item.fresh {
			ids = append(ids, report.ID)
		}
		for id := range o.unrecorded[key] {
			ids = append(ids, id)
		}
		o.mu.Unlock()
		if len(ids) > 0 {
			if err := o.store.RecordDeliveredRemoteReports(ctx, item.link.HostID, item.link.RemoteSessionID, ids, at); err != nil {
				o.log.Error("remote reports: record delivery failed", "host", item.host.label, "session", item.link.RemoteSessionID, "err", err)
				o.mu.Lock()
				if o.unrecorded[key] == nil {
					o.unrecorded[key] = map[string]bool{}
				}
				for _, id := range ids {
					o.unrecorded[key][id] = true
				}
				o.mu.Unlock()
			} else {
				o.mu.Lock()
				delete(o.unrecorded, key)
				o.mu.Unlock()
			}
		}
		hostID := item.link.HostID
		deadline := pass.ackDeadline(hostID, o.hostBudget)
		if pass.unreachable[hostID] || !time.Now().Before(deadline) || o.refusedPassword(hostID, item.host.password) {
			// The next pass acknowledges these again; the local or in-memory
			// record keeps them from being delivered twice.
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
	o.retainedMu.Lock()
	delete(o.lastRetainedPull, linkRef{hostID: link.HostID, session: link.RemoteSessionID})
	o.retainedMu.Unlock()
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
	if err := remotehost.UnattendedTransportAllowed(base, entry.AllowInsecureHTTP); err != nil {
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
	return &hostClient{
		http: client, base: base, hostID: hostID, password: entry.Password, label: label,
		pullerID: o.selfHostID, insecureOK: entry.AllowInsecureHTTP,
	}, nil
}

func (h *hostClient) close() { closeIdle(h.http) }

func closeIdle(client http.Client) {
	if transport, ok := client.Transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

type linkRef struct {
	hostID  string
	session domain.SessionID
}

// flushUnrecorded retries failed record writes each pass, even after the
// host stopped listing the reports, so the durable record is not lost.
func (o *Observer) flushUnrecorded(ctx context.Context) {
	o.mu.Lock()
	pending := make(map[linkRef][]string, len(o.unrecorded))
	for ref, set := range o.unrecorded {
		for id := range set {
			pending[ref] = append(pending[ref], id)
		}
	}
	o.mu.Unlock()
	at := o.now().UTC()
	for ref, ids := range pending {
		if err := o.store.RecordDeliveredRemoteReports(ctx, ref.hostID, ref.session, ids, at); err != nil {
			o.log.Error("remote reports: record retry failed", "host", ref.hostID, "session", ref.session, "err", err)
			continue
		}
		o.mu.Lock()
		for _, id := range ids {
			delete(o.unrecorded[ref], id)
		}
		if len(o.unrecorded[ref]) == 0 {
			delete(o.unrecorded, ref)
		}
		o.mu.Unlock()
	}
}

func (o *Observer) expiredSeenFor(ref linkRef) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.expiredSeen[ref]
}

// noteExpired logs the unseen expired reports, then records the total for the
// next pull. It runs only after a response, so a lost one is retried.
func (o *Observer) noteExpired(ref linkRef, label string, unseen, total int) {
	o.mu.Lock()
	o.expiredSeen[ref] = total
	o.mu.Unlock()
	if unseen > 0 {
		o.log.Warn("remote reports: reports expired before this daemon pulled them", "host", label, "session", ref.session, "expired", unseen)
	}
}

// noteLegacyHost warns once per host that predates per-puller acknowledgements.
// Such a host settles a report for every puller on the first acknowledgement.
func (o *Observer) noteLegacyHost(client *hostClient) {
	if client.pullerID == "" || client.perPuller {
		return
	}
	o.mu.Lock()
	first := !o.legacyWarned[client.hostID]
	o.legacyWarned[client.hostID] = true
	o.mu.Unlock()
	if first {
		o.log.Warn("remote reports: host predates per-puller acknowledgements; using first-delivery semantics", "host", client.label)
	}
}

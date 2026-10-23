package knowledgegraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/taskreport"
)

// Outcomes recorded for tasks that taskreport does not classify yet.
const (
	outcomeInProgress = "in_progress"
	outcomeTerminated = "terminated"
)

// ReportCreated queues a worker's done, stuck or needs-input report. It
// never blocks the report path; a full queue drops the event with a log.
func (s *Service) ReportCreated(rec domain.ReportRecord) {
	switch rec.State {
	case domain.ReportDone, domain.ReportStuck, domain.ReportNeedsInput:
	default:
		return
	}
	text := strings.TrimSpace(strings.Join(nonEmpty(rec.Note, rec.Message), " — "))
	s.enqueue(job{session: rec.SessionID, report: fmt.Sprintf("%s: %s", rec.State, text)})
}

// Subscribe queues every session that turns terminated.
func (s *Service) Subscribe(b *cdc.Broadcaster) (unsubscribe func()) {
	return b.Subscribe(func(e cdc.Event) {
		if e.Type != cdc.EventSessionUpdated || e.SessionID == "" {
			return
		}
		var p struct {
			IsTerminated bool `json:"isTerminated"`
		}
		if json.Unmarshal(e.Payload, &p) != nil || !p.IsTerminated {
			return
		}
		s.enqueue(job{session: domain.SessionID(e.SessionID)})
	})
}

func (s *Service) enqueue(j job) {
	select {
	case s.queue <- j:
	default:
		s.log.Warn("knowledge graph: queue full, dropping task event", "session", j.session)
	}
}

// embedInterval also catches nodes other hosts wrote or a failed embed.
const (
	embedInterval  = 5 * time.Minute
	embedBatchSize = 32
)

// Start drains the queue, and embeds new nodes when an embedder is set,
// until ctx ends; the channel closes when both loops stop.
func (s *Service) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-s.queue:
				if err := s.record(ctx, j); err != nil && ctx.Err() == nil {
					s.log.Warn("knowledge graph: record task failed", "session", j.session, "err", err)
				}
			}
		}
	}()
	if s.embedder != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.embedLoop(ctx, time.NewTicker(embedInterval).C)
		}()
		s.kickEmbed()
	}
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}

func (s *Service) kickEmbed() {
	select {
	case s.embedKick <- struct{}{}:
	default:
	}
}

func (s *Service) embedLoop(ctx context.Context, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.embedKick:
		case <-tick:
		}
		if err := s.embedMissing(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("knowledge graph: embedding backfill failed", "err", err)
		}
	}
}

// embedMissing embeds batches until no node lacks a vector of the
// model's size.
func (s *Service) embedMissing(ctx context.Context) error {
	if s.embedDims == 0 {
		probe, err := s.embedder.EmbedQuery(ctx, "dimension probe")
		if err != nil {
			return err
		}
		s.embedDims = len(probe)
	}
	for {
		targets, err := s.graph.MissingEmbeddings(ctx, embedBatchSize, s.embedDims)
		if err != nil || len(targets) == 0 {
			return err
		}
		texts := make([]string, len(targets))
		for i, t := range targets {
			texts[i] = t.Text
		}
		vecs, err := s.embedder.EmbedDocuments(ctx, texts)
		if err != nil {
			return err
		}
		if err := s.graph.SetEmbeddings(ctx, targets, vecs); err != nil {
			return err
		}
		if len(targets) < embedBatchSize {
			return nil
		}
	}
}

func (s *Service) record(ctx context.Context, j job) error {
	sess, err := s.sessions.Get(ctx, j.session)
	if err != nil {
		var ae *apierr.Error
		if errors.As(err, &ae) && ae.Kind == apierr.KindNotFound {
			return nil
		}
		return err
	}
	if sess.Kind != domain.KindWorker || sess.ProjectID == "" {
		return nil
	}
	fact := s.factFor(ctx, sess)
	fact.Report = j.report
	if j.report == "" && s.alreadyRecorded(sess.ID, fact.Outcome) {
		return nil
	}
	if err := s.graph.RecordTask(ctx, fact); err != nil {
		return err
	}
	s.mu.Lock()
	s.recorded[sess.ID] = fact.Outcome
	s.mu.Unlock()
	s.kickEmbed()
	return nil
}

func (s *Service) alreadyRecorded(id domain.SessionID, outcome string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recorded[id] == outcome
}

// factFor builds the task fact, enriched with the completed-task report
// when taskreport classifies the session.
func (s *Service) factFor(ctx context.Context, sess domain.Session) TaskFact {
	f := TaskFact{
		Key: s.taskKey(sess.ID), SessionID: string(sess.ID), Host: s.hostID,
		Project: string(sess.ProjectID), Title: sess.DisplayName, Issue: string(sess.IssueID),
		Outcome: outcomeInProgress, Harness: string(sess.Harness),
		Model: sess.Metadata.Model, Effort: sess.Metadata.Effort, Branch: sess.Metadata.Branch, At: s.now(),
	}
	if sess.IsTerminated {
		f.Outcome = outcomeTerminated
	}
	for _, pr := range sess.PRs {
		if pr.URL != "" {
			f.PRs = append(f.PRs, pr.URL)
		}
	}
	if s.tasks == nil {
		return f
	}
	rep, err := s.tasks.Get(ctx, sess.ProjectID, sess.ID)
	if err != nil {
		return f
	}
	f.Outcome = string(rep.Outcome)
	if rep.Title != "" {
		f.Title = rep.Title
	}
	if rep.ElapsedSeconds != nil {
		f.ElapsedSeconds = *rep.ElapsedSeconds
	}
	if rep.Tokens.InputTokens != nil {
		f.InputTokens = *rep.Tokens.InputTokens
	}
	if rep.Tokens.OutputTokens != nil {
		f.OutputTokens = *rep.Tokens.OutputTokens
	}
	f.Stages = stagesText(rep.Stages)
	return f
}

func stagesText(st taskreport.StageHistory) string {
	parts := make([]string, 0, len(st.Totals))
	for _, t := range st.Totals {
		parts = append(parts, fmt.Sprintf("%s %ds x%d", t.Stage, t.Seconds, t.Visits))
	}
	return strings.Join(parts, ", ")
}

func nonEmpty(values ...string) []string {
	out := values[:0:0]
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

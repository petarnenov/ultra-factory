package session

import (
	"context"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// KnowledgeGraph serves prior context from the central knowledge graph
// (ADR 0012). Nil disables it.
type KnowledgeGraph interface {
	PriorContext(ctx context.Context, project domain.ProjectID, issue domain.IssueID, text string) (string, []string, error)
	MarkServed(ctx context.Context, id domain.SessionID, lessonIDs []string) error
}

// knowledgeLookupTimeout keeps an unreachable graph from delaying spawns.
const knowledgeLookupTimeout = 3 * time.Second

// SetKnowledgeGraph enables spawn-time prior context.
func (s *Service) SetKnowledgeGraph(k KnowledgeGraph) {
	s.knowledge = k
}

// withKnowledgeContext adds prior context to a worker's spawn. It returns
// the served lesson ids; any failure leaves cfg unchanged.
func (s *Service) withKnowledgeContext(ctx context.Context, cfg ports.SpawnConfig, project domain.ProjectRecord) (ports.SpawnConfig, []string) {
	if s.knowledge == nil || cfg.KnowledgeContext != "" {
		return cfg, nil
	}
	if cfg.Kind != "" && cfg.Kind != domain.KindWorker {
		return cfg, nil
	}
	if strings.TrimSpace(cfg.Prompt) == "" && cfg.IssueID == "" {
		return cfg, nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, knowledgeLookupTimeout)
	defer cancel()
	text := cfg.Prompt + "\n" + cfg.IssueContext
	section, ids, err := s.knowledge.PriorContext(lookupCtx, domain.ProjectID(project.ID), cfg.IssueID, text)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("knowledge graph: prior context unavailable", "project", project.ID, "err", err)
		}
		return cfg, nil
	}
	cfg.KnowledgeContext = section
	return cfg, ids
}

// markKnowledgeServed links the new worker to its lessons off the spawn
// path.
func (s *Service) markKnowledgeServed(id domain.SessionID, lessonIDs []string) {
	if s.knowledge == nil || len(lessonIDs) == 0 {
		return
	}
	work := func() {
		ctx, cancel := context.WithTimeout(s.backgroundContext, 10*time.Second)
		defer cancel()
		if err := s.knowledge.MarkServed(ctx, id, lessonIDs); err != nil && s.logger != nil {
			s.logger.Warn("knowledge graph: mark served lessons failed", "session", id, "err", err)
		}
	}
	if s.runBackground != nil {
		s.runBackground(work)
		return
	}
	go work()
}

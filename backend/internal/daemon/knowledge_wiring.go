package daemon

import (
	"context"
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

type knowledgeGraphDeps struct {
	Getenv   func(string) string
	DataDir  string
	Sessions knowledgegraph.Sessions
	Tasks    knowledgegraph.TaskReports
	PRStore  knowledgeGraphPRStore
	HostID   string
	Events   *cdc.Broadcaster
	Log      *slog.Logger
}

type knowledgeGraphPRStore interface {
	ListChecks(context.Context, string) ([]domain.PullRequestCheck, error)
	ListPRComments(context.Context, string) ([]domain.PullRequestComment, error)
}

type knowledgeGraphPRFailures struct{ store knowledgeGraphPRStore }

func (f knowledgeGraphPRFailures) ForPR(ctx context.Context, prURL string) (knowledgegraph.PRFailureEvidence, error) {
	checks, err := f.store.ListChecks(ctx, prURL)
	if err != nil {
		return knowledgegraph.PRFailureEvidence{}, err
	}
	comments, err := f.store.ListPRComments(ctx, prURL)
	if err != nil {
		return knowledgegraph.PRFailureEvidence{}, err
	}
	out := knowledgegraph.PRFailureEvidence{}
	for _, check := range checks {
		if check.Status == domain.PRCheckFailed {
			out.Checks = append(out.Checks, knowledgegraph.FailedCheck{Name: check.Name, LogTail: check.LogTail})
		}
	}
	for _, comment := range comments {
		if !comment.Resolved && !comment.IsBot {
			out.Comments = append(out.Comments, knowledgegraph.UnresolvedComment{Author: comment.Author, File: comment.File, Body: comment.Body})
		}
	}
	return out, nil
}

// runningKnowledgeGraph is the started recorder plus its teardown.
type runningKnowledgeGraph struct {
	*knowledgegraph.Service
	unsubscribe func()
	cancel      context.CancelFunc
	done        <-chan struct{}
}

// Stop ends the recorder and waits for it.
func (k *runningKnowledgeGraph) Stop() {
	k.unsubscribe()
	k.cancel()
	<-k.done
}

// startKnowledgeGraph starts the recorder when a graph is configured (ADR
// 0012). Off or misconfigured, both results are nil and routes answer 501.
func startKnowledgeGraph(ctx context.Context, d knowledgeGraphDeps) (*runningKnowledgeGraph, controllers.KnowledgeService) {
	cfg, err := knowledgegraph.Load(d.Getenv, d.DataDir)
	if err != nil {
		d.Log.Warn("knowledge graph: invalid configuration, disabled", "err", err)
		return nil, nil
	}
	if !cfg.Enabled() {
		return nil, nil
	}
	var embedder knowledgegraph.Embedder
	if cfg.SemanticEnabled() {
		embedder = knowledgegraph.NewOllamaEmbedder(cfg, nil)
	}
	svc := knowledgegraph.New(knowledgegraph.Deps{
		Graph:    knowledgegraph.NewGraph(knowledgegraph.NewClient(cfg, nil)),
		Embedder: embedder, MinScore: cfg.MinScore, MergeScore: cfg.MergeScore,
		Sessions: d.Sessions, Tasks: d.Tasks, Failures: knowledgeGraphPRFailures{store: d.PRStore},
		Paths: knowledgeChangedPaths{}, HostID: d.HostID, Logger: d.Log,
	})
	runCtx, cancel := context.WithCancel(ctx)
	k := &runningKnowledgeGraph{Service: svc, cancel: cancel}
	k.unsubscribe = svc.Subscribe(d.Events)
	k.done = svc.Start(runCtx)
	d.Log.Info("knowledge graph: enabled", "url", cfg.URL, "database", cfg.Database,
		"semantic", cfg.SemanticEnabled(), "embeddingModel", cfg.EmbeddingModel, "mergeScore", cfg.MergeScore)
	return k, svc
}

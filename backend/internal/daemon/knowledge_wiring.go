package daemon

import (
	"context"
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

type knowledgeGraphDeps struct {
	Getenv   func(string) string
	DataDir  string
	Sessions knowledgegraph.Sessions
	Tasks    knowledgegraph.TaskReports
	HostID   string
	Events   *cdc.Broadcaster
	Log      *slog.Logger
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
	svc := knowledgegraph.New(knowledgegraph.Deps{
		Graph:    knowledgegraph.NewGraph(knowledgegraph.NewClient(cfg, nil)),
		Sessions: d.Sessions, Tasks: d.Tasks, HostID: d.HostID, Logger: d.Log,
	})
	runCtx, cancel := context.WithCancel(ctx)
	k := &runningKnowledgeGraph{Service: svc, cancel: cancel}
	k.unsubscribe = svc.Subscribe(d.Events)
	k.done = svc.Start(runCtx)
	d.Log.Info("knowledge graph: enabled", "url", cfg.URL, "database", cfg.Database)
	return k, svc
}

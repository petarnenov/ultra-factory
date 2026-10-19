package sqlite

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestProjectGitHubAccountSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	row := domain.ProjectRecord{
		ID:   "widgets",
		Path: "/repo/widgets",
		Config: domain.ProjectConfig{
			SCM: domain.SCMConfig{GitHub: domain.GitHubSCMConfig{Account: "work-account"}},
		},
	}
	if err := store.UpsertProject(ctx, row); err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	got, ok, err := reopened.GetProject(ctx, "widgets")
	if err != nil || !ok {
		t.Fatalf("GetProject = (%+v, %v, %v)", got, ok, err)
	}
	if account := got.Config.GitHubAccount(); account != "work-account" {
		t.Fatalf("account after reopen = %q, want work-account", account)
	}
}

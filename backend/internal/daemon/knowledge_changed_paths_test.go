package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestKnowledgeChangedPathsUsesOriginTargetMergeBase(t *testing.T) {
	repo := initChangedPathsRepo(t)
	gitChangedPaths(t, repo, "branch", "work")
	if err := os.WriteFile(filepath.Join(repo, "backend.go"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitChangedPaths(t, repo, "add", "backend.go")
	gitChangedPaths(t, repo, "commit", "-m", "change")
	got, err := (knowledgeChangedPaths{}).ChangedPaths(context.Background(), domain.Session{SessionRecord: domain.SessionRecord{Metadata: domain.SessionMetadata{WorkspacePath: repo}}}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "backend.go" {
		t.Fatalf("paths = %v, want backend.go", got)
	}
}

func TestKnowledgeChangedPathsMissingWorkspaceAndGitFailures(t *testing.T) {
	ctx := context.Background()
	missing, err := (knowledgeChangedPaths{}).ChangedPaths(ctx, domain.Session{SessionRecord: domain.SessionRecord{Metadata: domain.SessionMetadata{WorkspacePath: filepath.Join(t.TempDir(), "gone")}}}, "dev")
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing workspace = %v, %v; want no paths and no error", missing, err)
	}
	repo := initChangedPathsRepo(t)
	_, err = (knowledgeChangedPaths{}).ChangedPaths(ctx, domain.Session{SessionRecord: domain.SessionRecord{Metadata: domain.SessionMetadata{WorkspacePath: repo}}}, "absent")
	if err == nil {
		t.Fatal("missing target ref returned no error")
	}
}

func initChangedPathsRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitChangedPaths(t, repo, "init", "--initial-branch=dev")
	gitChangedPaths(t, repo, "config", "user.email", "test@example.com")
	gitChangedPaths(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitChangedPaths(t, repo, "add", "base.txt")
	gitChangedPaths(t, repo, "commit", "-m", "base")
	gitChangedPaths(t, repo, "update-ref", "refs/remotes/origin/dev", "HEAD")
	return repo
}

func gitChangedPaths(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

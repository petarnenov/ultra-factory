package daemon

import (
	"context"
	"os"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// knowledgeChangedPaths runs the narrow git reads used by task recording.
type knowledgeChangedPaths struct{}

func (knowledgeChangedPaths) ChangedPaths(ctx context.Context, sess domain.Session, target string) ([]string, error) {
	if strings.TrimSpace(sess.Metadata.WorkspacePath) == "" || strings.TrimSpace(target) == "" {
		return nil, nil
	}
	if _, err := os.Stat(sess.Metadata.WorkspacePath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	run := func(args ...string) (string, error) {
		cmd := aoprocess.CommandContext(ctx, "git", args...)
		out, err := cmd.Output()
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	base, err := run("-C", sess.Metadata.WorkspacePath, "merge-base", "origin/"+target, "HEAD")
	if err != nil {
		return nil, err
	}
	out, err := run("-C", sess.Metadata.WorkspacePath, "diff", "--name-only", base+"...HEAD")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return []string{}, nil
	}
	return strings.Split(out, "\n"), nil
}

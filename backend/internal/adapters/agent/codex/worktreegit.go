package codex

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// codexGitMetadataWritableArgs grants a linked worktree access to its private
// gitdir and shared repository data; sibling worktree gitdirs remain excluded.
func codexGitMetadataWritableArgs(ctx context.Context, workspace string) ([]string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil, nil
	}
	gitDir, err := gitPath(ctx, workspace, "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	commonDir, err := gitPath(ctx, workspace, "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	if gitDir == "" || commonDir == "" {
		return nil, nil
	}
	gitDir, err = filepath.Abs(gitDir)
	if err != nil {
		return nil, fmt.Errorf("codex: resolve worktree gitdir: %w", err)
	}
	commonDir, err = filepath.Abs(commonDir)
	if err != nil {
		return nil, fmt.Errorf("codex: resolve common gitdir: %w", err)
	}
	rel, err := filepath.Rel(commonDir, gitDir)
	if err != nil {
		return nil, fmt.Errorf("codex: compare worktree and common gitdirs: %w", err)
	}
	if filepath.IsAbs(rel) || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel != filepath.Join("worktrees", filepath.Base(gitDir)) {
		return nil, nil
	}
	roots := []string{
		gitDir,
		filepath.Join(commonDir, "refs"),
		filepath.Join(commonDir, "objects"),
		filepath.Join(commonDir, "logs"),
		filepath.Join(commonDir, "FETCH_HEAD"),
	}
	quoted := make([]string, 0, len(roots))
	for _, root := range roots {
		quoted = append(quoted, strconv.Quote(root))
	}
	return []string{"-c", "sandbox_workspace_write.writable_roots=[" + strings.Join(quoted, ",") + "]"}, nil
}

func gitPath(ctx context.Context, workspace string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", workspace, "rev-parse"}, args...)
	output, err := aoprocess.CommandContext(ctx, "git", cmdArgs...).Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", nil
	}
	return strings.TrimSpace(string(output)), nil
}

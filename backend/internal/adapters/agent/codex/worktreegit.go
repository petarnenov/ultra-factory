package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
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
	cmd := aoprocess.CommandContext(ctx, "git", cmdArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if strings.Contains(stderr.String(), "not a git repository") {
			hasMetadata, markerErr := workspaceHasGitMetadata(workspace)
			if markerErr != nil {
				return "", markerErr
			}
			if !hasMetadata {
				return "", nil
			}
		}
		return "", fmt.Errorf("codex: git rev-parse %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(output)), nil
}

func workspaceHasGitMetadata(workspace string) (bool, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return false, fmt.Errorf("codex: resolve workspace for Git metadata check: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return false, fmt.Errorf("codex: inspect workspace for Git metadata: %w", err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("codex: workspace %q is not a directory", workspace)
	}
	_, err = os.Lstat(filepath.Join(abs, ".git"))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("codex: inspect Git metadata marker: %w", err)
}

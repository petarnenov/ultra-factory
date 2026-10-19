package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// CheckoutRequest identifies one review pass's checkout. Key is the batch (or
// run) id that a disposable worktree is created and released under.
type CheckoutRequest struct {
	Key           string
	WorkspacePath string
	TargetSHA     string
	PRURL         string
}

// PreparedCheckout is the verified tree a reviewer must read.
type PreparedCheckout struct {
	Key        string
	Path       string
	RepoPath   string
	SHA        string
	Tree       domain.ReviewCheckoutTree
	Disposable bool
}

// CheckoutManager verifies the worker checkout at a target commit and
// materializes a disposable detached worktree when it cannot be used.
type CheckoutManager interface {
	Prepare(ctx context.Context, req CheckoutRequest) (PreparedCheckout, error)
	// Reopen returns an existing disposable checkout for restoring a pass whose
	// reviewer pane died. It must verify the worktree still exists at its SHA.
	Reopen(ctx context.Context, key string) (PreparedCheckout, error)
	Release(ctx context.Context, key string) error
}

// CheckoutReleaser releases a disposable checkout once its pass is terminal.
type CheckoutReleaser interface {
	Release(ctx context.Context, key string) error
}

// workerCheckout is the no-op manager used by tests and callers that do not
// wire Git. Production always wires GitCheckout so checkout verification runs.
type workerCheckout struct{}

func (workerCheckout) Prepare(_ context.Context, req CheckoutRequest) (PreparedCheckout, error) {
	if strings.TrimSpace(req.WorkspacePath) == "" {
		return PreparedCheckout{}, errors.New("review worker session has no workspace")
	}
	return PreparedCheckout{
		Key:      req.Key,
		Path:     req.WorkspacePath,
		RepoPath: req.WorkspacePath,
		SHA:      req.TargetSHA,
		Tree:     domain.ReviewCheckoutWorker,
	}, nil
}

func (workerCheckout) Release(context.Context, string) error { return nil }

func (workerCheckout) Reopen(context.Context, string) (PreparedCheckout, error) {
	return PreparedCheckout{}, errors.New("no disposable review checkout to restore")
}

// GitCheckout is the production checkout manager. It never writes to the
// worker's checkout and confines disposable worktrees to AO's data directory.
type GitCheckout struct {
	dataDir string
	gitBin  string
}

// NewGitCheckout builds the Git-backed checkout manager under dataDir.
func NewGitCheckout(dataDir string) *GitCheckout {
	return &GitCheckout{dataDir: strings.TrimSpace(dataDir), gitBin: "git"}
}

// checkoutMetadata makes release possible after a daemon restart without
// keeping repository paths in the database or the reviewer prompt.
type checkoutMetadata struct {
	RepoPath     string `json:"repoPath"`
	WorktreePath string `json:"worktreePath"`
	SHA          string `json:"sha"`
}

// Prepare returns the verified checkout for one review pass. It uses the
// worker tree when HEAD is the target and clean, else a detached worktree.
func (g *GitCheckout) Prepare(ctx context.Context, req CheckoutRequest) (PreparedCheckout, error) {
	workspace := strings.TrimSpace(req.WorkspacePath)
	target := strings.TrimSpace(req.TargetSHA)
	if workspace == "" {
		return PreparedCheckout{}, errors.New("review worker session has no workspace")
	}
	if target == "" {
		return PreparedCheckout{}, errors.New("review target SHA is required to verify the checkout")
	}
	head, err := g.gitOutput(ctx, workspace, "rev-parse", "HEAD")
	if err != nil {
		return PreparedCheckout{}, fmt.Errorf("read worker checkout HEAD: %w", err)
	}
	status, err := g.gitOutput(ctx, workspace, "status", "--porcelain")
	if err != nil {
		return PreparedCheckout{}, fmt.Errorf("read worker checkout status: %w", err)
	}
	if strings.EqualFold(head, target) && status == "" {
		return PreparedCheckout{
			Key:      req.Key,
			Path:     workspace,
			RepoPath: workspace,
			SHA:      strings.ToLower(target),
			Tree:     domain.ReviewCheckoutWorker,
		}, nil
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		return PreparedCheckout{}, errors.New("review checkout key is required for a disposable worktree")
	}
	if g.dataDir == "" {
		return PreparedCheckout{}, g.mismatch(ctx, workspace, head, target, "AO has no data directory")
	}
	if err := g.ensureCommit(ctx, workspace, target, req.PRURL); err != nil {
		return PreparedCheckout{}, err
	}
	// A previous daemon may have left this key behind. Release is idempotent.
	_ = g.Release(ctx, key)
	path := g.worktreePath(key)
	if err := os.MkdirAll(g.checkoutRoot(), 0o700); err != nil {
		return PreparedCheckout{}, fmt.Errorf("create review checkout directory: %w", err)
	}
	if out, err := g.git(ctx, workspace, "worktree", "add", "--detach", path, target); err != nil {
		return PreparedCheckout{}, fmt.Errorf("materialize disposable review worktree at %s: %s", shortCheckoutSHA(target), commandDetail(out, err))
	}
	verified, verifyErr := g.gitOutput(ctx, path, "rev-parse", "HEAD")
	if verifyErr != nil || !strings.EqualFold(verified, target) {
		_ = os.RemoveAll(path)
		_, _ = g.git(ctx, workspace, "worktree", "prune")
		if verifyErr != nil {
			return PreparedCheckout{}, fmt.Errorf("verify disposable review worktree: %w", verifyErr)
		}
		return PreparedCheckout{}, fmt.Errorf("disposable review worktree is at %s, want %s", shortCheckoutSHA(verified), shortCheckoutSHA(target))
	}
	if err := writeCheckoutMetadata(g.metadataPath(key), checkoutMetadata{RepoPath: workspace, WorktreePath: path, SHA: strings.ToLower(target)}); err != nil {
		_ = os.RemoveAll(path)
		_, _ = g.git(ctx, workspace, "worktree", "prune")
		return PreparedCheckout{}, err
	}
	return PreparedCheckout{
		Key:        key,
		Path:       path,
		RepoPath:   workspace,
		SHA:        strings.ToLower(target),
		Tree:       domain.ReviewCheckoutDisposable,
		Disposable: true,
	}, nil
}

// Reopen returns an existing disposable checkout for restoring a pass after
// its reviewer pane died, verifying the worktree still matches the stored SHA.
func (g *GitCheckout) Reopen(ctx context.Context, key string) (PreparedCheckout, error) {
	key = strings.TrimSpace(key)
	if g == nil || g.dataDir == "" || key == "" {
		return PreparedCheckout{}, errors.New("review checkout key is required to restore a review")
	}
	meta, err := readCheckoutMetadata(g.metadataPath(key))
	if err != nil {
		return PreparedCheckout{}, fmt.Errorf("review checkout for %s is no longer available: %w", shortCheckoutSHA(key), err)
	}
	head, err := g.gitOutput(ctx, meta.WorktreePath, "rev-parse", "HEAD")
	if err != nil || !strings.EqualFold(head, meta.SHA) {
		return PreparedCheckout{}, fmt.Errorf("review checkout %s is at %s, want %s", meta.WorktreePath, shortCheckoutSHA(head), shortCheckoutSHA(meta.SHA))
	}
	return PreparedCheckout{
		Key:        key,
		Path:       meta.WorktreePath,
		RepoPath:   meta.RepoPath,
		SHA:        meta.SHA,
		Tree:       domain.ReviewCheckoutDisposable,
		Disposable: true,
	}, nil
}

// Release removes a disposable checkout and its metadata. It is idempotent
// and safe to call after the worktree is already gone.
func (g *GitCheckout) Release(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if g == nil || g.dataDir == "" || key == "" {
		return nil
	}
	path := g.worktreePath(key)
	metaPath := g.metadataPath(key)
	meta, err := readCheckoutMetadata(metaPath)
	if errors.Is(err, os.ErrNotExist) {
		return os.RemoveAll(path)
	}
	if err != nil {
		return err
	}
	// Cleanup must finish even when the request that triggered it is cancelled.
	removeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var removeErr error
	if meta.RepoPath != "" && meta.WorktreePath != "" {
		if out, gitErr := g.git(removeCtx, meta.RepoPath, "worktree", "remove", "--force", meta.WorktreePath); gitErr != nil {
			removeErr = fmt.Errorf("remove review worktree: %s", commandDetail(out, gitErr))
			_ = os.RemoveAll(meta.WorktreePath)
			_, _ = g.git(removeCtx, meta.RepoPath, "worktree", "prune")
		}
	} else {
		_ = os.RemoveAll(path)
	}
	if err := os.Remove(metaPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		removeErr = errors.Join(removeErr, err)
	}
	return removeErr
}

func (g *GitCheckout) ensureCommit(ctx context.Context, workspace, target, prURL string) error {
	if g.commitExists(ctx, workspace, target) {
		return nil
	}
	// Fetch the exact commit first: providers that allow reachable-SHA fetches
	// return a PR head even when it lives on a fork.
	if _, err := g.git(ctx, workspace, "fetch", "--no-tags", "origin", target); err == nil && g.commitExists(ctx, workspace, target) {
		return nil
	}
	if ref := pullHeadRef(prURL); ref != "" {
		if _, err := g.git(ctx, workspace, "fetch", "--no-tags", "origin", ref); err == nil && g.commitExists(ctx, workspace, target) {
			return nil
		}
	}
	if _, err := g.git(ctx, workspace, "fetch", "--no-tags", "origin"); err == nil && g.commitExists(ctx, workspace, target) {
		return nil
	}
	head, _ := g.gitOutput(ctx, workspace, "rev-parse", "HEAD")
	return g.mismatch(ctx, workspace, head, target, "target is not available from origin")
}

func (g *GitCheckout) mismatch(ctx context.Context, workspace, head, target, reason string) error {
	state := "clean"
	if status, err := g.gitOutput(ctx, workspace, "status", "--porcelain"); err == nil && status != "" {
		state = "has uncommitted changes"
	}
	return fmt.Errorf("review checkout mismatch: worker checkout is at %s and %s, target %s cannot be verified (%s)",
		shortCheckoutSHA(head), state, shortCheckoutSHA(target), reason)
}

func (g *GitCheckout) commitExists(ctx context.Context, dir, sha string) bool {
	_, err := g.git(ctx, dir, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

func (g *GitCheckout) checkoutRoot() string {
	return filepath.Join(g.dataDir, "review-checkouts")
}

func (g *GitCheckout) worktreePath(key string) string {
	return filepath.Join(g.checkoutRoot(), safeCheckoutKey(key))
}

func (g *GitCheckout) metadataPath(key string) string {
	return g.worktreePath(key) + ".json"
}

func (g *GitCheckout) git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	full := append([]string{"-C", dir}, args...)
	return aoprocess.CommandContext(ctx, g.gitBin, full...).CombinedOutput()
}

func (g *GitCheckout) gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := g.git(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func writeCheckoutMetadata(path string, meta checkoutMetadata) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("encode review checkout metadata: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write review checkout metadata: %w", err)
	}
	return nil
}

func readCheckoutMetadata(path string) (checkoutMetadata, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return checkoutMetadata{}, err
	}
	var meta checkoutMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return checkoutMetadata{}, fmt.Errorf("decode review checkout metadata: %w", err)
	}
	return meta, nil
}

// safeCheckoutKey keeps an id usable as a directory name.
func safeCheckoutKey(key string) string {
	var b strings.Builder
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 || b.String() == "." || b.String() == ".." {
		return "review"
	}
	return b.String()
}

// pullHeadRef maps a provider PR URL to the ref a plain fetch can request.
func pullHeadRef(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		switch parts[i] {
		case "pull", "pulls":
			return "refs/pull/" + parts[i+1] + "/head"
		case "merge_requests":
			return "refs/merge-requests/" + parts[i+1] + "/head"
		}
	}
	return ""
}

func shortCheckoutSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 12 {
		return sha[:12]
	}
	if sha == "" {
		return "<unknown>"
	}
	return sha
}

func commandDetail(out []byte, err error) string {
	detail := strings.TrimSpace(string(out))
	if len(detail) > 400 {
		detail = strings.TrimSpace(detail[:400]) + "..."
	}
	if detail == "" {
		return err.Error()
	}
	return detail
}

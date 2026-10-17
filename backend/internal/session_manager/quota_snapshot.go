package sessionmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	quotaSnapshotThresholdPercent = 10.0
	quotaObservationMaxAge        = 5 * time.Minute
	quotaSnapshotMaxBytes         = 128 << 10
)

// QuotaSnapshotSource identifies the live provider generation that emitted an
// exact account quota observation. Chat generations are controller-fenced;
// TUI generations are fenced by the native root and current runtime launch.
type QuotaSnapshotSource struct {
	Harness              domain.AgentHarness
	Generation           string
	NativeRootID         string
	NativeTranscriptPath string
	Controller           bool
}

type quotaSnapshot struct {
	SchemaVersion    int                  `json:"schemaVersion"`
	SessionID        domain.SessionID     `json:"sessionId"`
	Harness          domain.AgentHarness  `json:"harness"`
	Generation       string               `json:"generation"`
	RemainingPercent float64              `json:"remainingPercent"`
	ObservedAt       time.Time            `json:"observedAt"`
	CapturedAt       time.Time            `json:"capturedAt"`
	OriginalTask     string               `json:"originalTask,omitempty"`
	LatestUserPrompt string               `json:"latestUserPrompt,omitempty"`
	AssistantUpdate  string               `json:"latestAssistantUpdate,omitempty"`
	Workspaces       []quotaWorkspaceFact `json:"workspaces,omitempty"`
	PullRequests     []switchPRFact       `json:"pullRequests,omitempty"`
}

type quotaWorkspaceFact struct {
	Repository string `json:"repository,omitempty"`
	Path       string `json:"path"`
	Branch     string `json:"branch,omitempty"`
	HeadSHA    string `json:"headSha,omitempty"`
	Dirty      bool   `json:"dirty"`
	Staged     bool   `json:"staged"`
	Untracked  bool   `json:"untracked"`
}

// CodexQuotaRemaining extracts the lowest exact remaining value across the
// provider's overall and additional rate-limit windows. Missing windows are
// ignored; an observation with no usable percentages remains unknown.
func CodexQuotaRemaining(observation ports.CodexCapacityObservation) (float64, bool) {
	remaining := 101.0
	consume := func(bucket *domain.CodexCapacityBucket) {
		if bucket == nil {
			return
		}
		for _, window := range []*domain.CodexCapacityWindow{bucket.Primary, bucket.Secondary} {
			if window != nil && window.UsedPercent >= 0 && window.UsedPercent <= 100 {
				if value := 100 - window.UsedPercent; value < remaining {
					remaining = value
				}
			}
		}
	}
	consume(observation.Overall)
	for i := range observation.AdditionalBuckets {
		consume(&observation.AdditionalBuckets[i])
	}
	return remaining, remaining <= 100
}

// ObserveQuotaAndSnapshot persists one immutable bounded context snapshot when
// an authoritative observation crosses the low-quota threshold. Unknown,
// stale, future-dated, superseded, or terminated sessions are ignored.
func (m *Manager) ObserveQuotaAndSnapshot(ctx context.Context, id domain.SessionID, source QuotaSnapshotSource, remaining float64, observedAt time.Time) error {
	now := m.clock()
	if remaining < 0 || remaining > quotaSnapshotThresholdPercent || observedAt.IsZero() || observedAt.After(now.Add(time.Minute)) || now.Sub(observedAt) > quotaObservationMaxAge {
		return nil
	}
	if (source.Harness != domain.HarnessCodex && (!source.Controller || source.Harness != domain.HarnessClaudeCode)) || strings.TrimSpace(source.Generation) == "" {
		return nil
	}
	rec, found, err := m.store.GetSession(ctx, id)
	if err != nil || !found || rec.IsTerminated || rec.Harness != source.Harness {
		return err
	}
	if source.Controller {
		if rec.Metadata.ControllerGeneration != source.Generation {
			return nil
		}
	} else {
		if strings.TrimSpace(source.NativeRootID) == "" || strings.TrimSpace(source.NativeTranscriptPath) == "" ||
			rec.Metadata.AgentSessionID != source.NativeRootID || rec.Metadata.RuntimeLaunchID != source.Generation ||
			filepath.Clean(rec.Metadata.NativeTranscriptPath) != filepath.Clean(source.NativeTranscriptPath) {
			return nil
		}
	}
	if strings.TrimSpace(m.dataDir) == "" {
		return errors.New("quota snapshot: AO data directory is unavailable")
	}
	if found, err := m.hasMatchingQuotaSnapshot(ctx, id, source); err != nil {
		return err
	} else if found {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	workspaces := m.captureWorkspaceFacts(ctx, rec)
	compact := make([]quotaWorkspaceFact, 0, len(workspaces))
	for _, fact := range workspaces {
		compact = append(compact, quotaWorkspaceFact{
			Repository: fact.Repository, Path: fact.Path, Branch: fact.Branch, HeadSHA: fact.HeadSHA,
			Dirty: fact.Dirty, Staged: fact.Staged, Untracked: fact.Untracked,
		})
	}
	snapshot := quotaSnapshot{
		SchemaVersion: 1, SessionID: id, Harness: source.Harness, Generation: source.Generation,
		RemainingPercent: remaining, ObservedAt: observedAt.UTC(), CapturedAt: now.UTC(),
		OriginalTask: boundedQuotaText(rec.Metadata.Prompt), LatestUserPrompt: boundedQuotaText(rec.Metadata.LatestUserPrompt),
		AssistantUpdate: boundedQuotaText(rec.Metadata.LatestAssistantUpdate), Workspaces: compact,
		PullRequests: m.capturePRFacts(ctx, id),
	}
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("quota snapshot: encode: %w", err)
	}
	body = append(body, '\n')
	if len(body) > quotaSnapshotMaxBytes {
		return errors.New("quota snapshot: bounded context exceeded its size limit")
	}
	root := filepath.Join(m.dataDir, "quota-snapshots")
	sessionDir := filepath.Join(root, string(id))
	if !handoffPathComponent(string(id)) {
		return errors.New("quota snapshot: invalid session identity")
	}
	dataInfo, err := os.Lstat(m.dataDir)
	if err != nil || !dataInfo.IsDir() || dataInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("quota snapshot: AO data path is not a real directory")
	}
	for _, dir := range []string{root, sessionDir} {
		if err := ensurePrivateHandoffDirectory(dir); err != nil {
			return err
		}
	}
	if !validateQuotaSnapshotDirectory(m.dataDir, root, sessionDir) {
		return errors.New("quota snapshot: storage path is not a real AO-owned directory")
	}
	generationHash := sha256.Sum256([]byte(source.Generation))
	path := filepath.Join(sessionDir, hex.EncodeToString(generationHash[:])+".json")
	if existing, found, readErr := readRegularFileWithoutSymlink(ctx, path, quotaSnapshotMaxBytes+1); readErr != nil {
		return fmt.Errorf("quota snapshot: inspect existing snapshot: %w", readErr)
	} else if found {
		if validQuotaSnapshot(existing, id, source) {
			return nil
		}
		return errors.New("quota snapshot: existing snapshot identity does not match source generation")
	}
	if err := writeAtomicImmutableFile(ctx, path, body); err != nil {
		if existing, found, readErr := readRegularFileWithoutSymlink(ctx, path, quotaSnapshotMaxBytes+1); readErr == nil && found && validQuotaSnapshot(existing, id, source) {
			return nil
		}
		return fmt.Errorf("quota snapshot: persist: %w", err)
	}
	return nil
}

func (m *Manager) hasMatchingQuotaSnapshot(ctx context.Context, id domain.SessionID, source QuotaSnapshotSource) (bool, error) {
	if !handoffPathComponent(string(id)) {
		return false, errors.New("quota snapshot: invalid session identity")
	}
	root := filepath.Join(m.dataDir, "quota-snapshots")
	dir := filepath.Join(root, string(id))
	rootInfo, rootErr := os.Lstat(root)
	if errors.Is(rootErr, os.ErrNotExist) {
		return false, nil
	}
	if rootErr != nil {
		return false, rootErr
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("quota snapshot: storage root is not a real directory")
	}
	dirInfo, dirErr := os.Lstat(dir)
	if errors.Is(dirErr, os.ErrNotExist) {
		return false, nil
	}
	if dirErr != nil {
		return false, dirErr
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("quota snapshot: session storage path is not a real directory")
	}
	hash := sha256.Sum256([]byte(source.Generation))
	path := filepath.Join(dir, hex.EncodeToString(hash[:])+".json")
	body, found, err := readRegularFileWithoutSymlink(ctx, path, quotaSnapshotMaxBytes+1)
	if err != nil || !found {
		return false, err
	}
	if !validQuotaSnapshot(body, id, source) {
		return false, errors.New("quota snapshot: existing snapshot identity does not match source generation")
	}
	return true, nil
}

func validQuotaSnapshot(body []byte, id domain.SessionID, source QuotaSnapshotSource) bool {
	var existing quotaSnapshot
	return len(body) <= quotaSnapshotMaxBytes && json.Unmarshal(body, &existing) == nil &&
		existing.SchemaVersion == 1 && existing.SessionID == id && existing.Harness == source.Harness && existing.Generation == source.Generation
}

// fillFromQuotaSnapshot fills the facts a stopped session left empty from its
// latest quota snapshot.
func (c *deterministicSwitchContext) fillFromQuotaSnapshot(quota quotaSnapshot) {
	if strings.TrimSpace(c.OriginalTask) == "" {
		c.OriginalTask = quota.OriginalTask
	}
	if strings.TrimSpace(c.LatestUserPrompt) == "" {
		c.LatestUserPrompt = quota.LatestUserPrompt
	}
	if strings.TrimSpace(c.LatestAssistantUpdate) == "" {
		c.LatestAssistantUpdate = quota.AssistantUpdate
	}
	if len(c.Workspaces) == 0 {
		for _, fact := range quota.Workspaces {
			c.Workspaces = append(c.Workspaces, switchWorkspaceFact{Repository: fact.Repository, Path: fact.Path, Branch: fact.Branch, HeadSHA: fact.HeadSHA, Dirty: fact.Dirty, Staged: fact.Staged, Untracked: fact.Untracked})
		}
	}
	if len(c.PullRequests) == 0 {
		c.PullRequests = quota.PullRequests
	}
}

func (m *Manager) readQuotaSnapshot(ctx context.Context, id domain.SessionID, harness domain.AgentHarness, generation string) (quotaSnapshot, bool) {
	if strings.TrimSpace(generation) == "" || !handoffPathComponent(string(id)) || strings.TrimSpace(m.dataDir) == "" {
		return quotaSnapshot{}, false
	}
	hash := sha256.Sum256([]byte(generation))
	path := filepath.Join(m.dataDir, "quota-snapshots", string(id), hex.EncodeToString(hash[:])+".json")
	body, found, err := readRegularFileWithoutSymlink(ctx, path, quotaSnapshotMaxBytes+1)
	if err != nil || !found {
		return quotaSnapshot{}, false
	}
	source := QuotaSnapshotSource{Harness: harness, Generation: generation}
	if !validQuotaSnapshot(body, id, source) {
		return quotaSnapshot{}, false
	}
	var snapshot quotaSnapshot
	if json.Unmarshal(body, &snapshot) != nil {
		return quotaSnapshot{}, false
	}
	return snapshot, true
}

func (m *Manager) removeQuotaSnapshot(ctx context.Context, id domain.SessionID, generation string) error {
	if strings.TrimSpace(generation) == "" || !handoffPathComponent(string(id)) || strings.TrimSpace(m.dataDir) == "" {
		return nil
	}
	hash := sha256.Sum256([]byte(generation))
	root := filepath.Join(m.dataDir, "quota-snapshots")
	dir := filepath.Join(root, string(id))
	if !validateQuotaSnapshotDirectory(m.dataDir, root, dir) {
		return nil
	}
	path := filepath.Join(dir, hex.EncodeToString(hash[:])+".json")
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("quota snapshot: remove after completed handoff: %w", err)
	}
	return syncHandoffDirectory(ctx, dir)
}

func validateQuotaSnapshotDirectory(dataDir, root, sessionDir string) bool {
	for _, dir := range []string{dataDir, root, sessionDir} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

func boundedQuotaText(value string) string {
	value = strings.TrimSpace(value)
	const limit = 16 << 10
	if len(value) > limit {
		value = value[len(value)-limit:]
	}
	return normalizeHistoricalContext([]byte(value))
}

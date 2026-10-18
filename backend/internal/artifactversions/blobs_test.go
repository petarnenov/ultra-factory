package artifactversions

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/sessionartifacts"
)

func newBlobStoreForTest(t *testing.T) (string, blobStore) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return dir, blobStore{root: root}
}

func TestBlobStoreStageHashesAndSizes(t *testing.T) {
	_, bs := newBlobStoreForTest(t)
	const body = "hello blob"
	s, err := bs.stage(strings.NewReader(body), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if s.sum != sum(body) {
		t.Fatalf("sum = %s, want %s", s.sum, sum(body))
	}
	if s.size != int64(len(body)) {
		t.Fatalf("size = %d, want %d", s.size, len(body))
	}
	if _, err := bs.root.Stat(s.tmp); err != nil {
		t.Fatalf("staged temp missing: %v", err)
	}
	bs.discard(s)
	if _, err := bs.root.Stat(s.tmp); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("staged temp not removed: %v", err)
	}
}

func TestBlobStoreStageRejectsOversize(t *testing.T) {
	_, bs := newBlobStoreForTest(t)
	_, err := bs.stage(strings.NewReader("ab"), 1)
	if !errors.Is(err, errTooLarge) {
		t.Fatalf("err = %v, want errTooLarge", err)
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestBlobStoreStageReaderError(t *testing.T) {
	_, bs := newBlobStoreForTest(t)
	want := errors.New("boom")
	_, err := bs.stage(errReader{err: want}, 10)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestBlobStoreStageMkdirError(t *testing.T) {
	dir, bs := newBlobStoreForTest(t)
	if err := os.WriteFile(filepath.Join(dir, sessionartifacts.VersionsDir), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := bs.stage(strings.NewReader("x"), 10)
	if err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestBlobStoreCreateIncomingPermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission tests require non-root")
	}
	dir, bs := newBlobStoreForTest(t)
	if err := bs.root.MkdirAll(blobsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	blobsAbs := filepath.Join(dir, blobsDir)
	if err := os.Chmod(blobsAbs, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blobsAbs, 0o755) })
	_, _, err := bs.createIncoming()
	if err == nil {
		t.Fatal("expected permission error")
	}
}

func TestBlobStoreCommitDeduplicates(t *testing.T) {
	_, bs := newBlobStoreForTest(t)
	s1, err := bs.stage(strings.NewReader("x"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := bs.commit(s1); err != nil {
		t.Fatal(err)
	}
	s2, err := bs.stage(strings.NewReader("x"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := bs.commit(s2); err != nil {
		t.Fatal(err)
	}
	if _, err := bs.root.Stat(s2.tmp); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("duplicate staged temp not removed: %v", err)
	}
}

func TestBlobStoreCommitMkdirError(t *testing.T) {
	dir, bs := newBlobStoreForTest(t)
	s, err := bs.stage(strings.NewReader("x"), 10)
	if err != nil {
		t.Fatal(err)
	}
	shard := filepath.Join(blobsDir, s.sum[:2])
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, shard)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, shard), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = bs.commit(s)
	if err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestBlobStoreCommitRenameError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission tests require non-root")
	}
	dir, bs := newBlobStoreForTest(t)
	s, err := bs.stage(strings.NewReader("x"), 10)
	if err != nil {
		t.Fatal(err)
	}
	shardRel := filepath.Join(blobsDir, s.sum[:2])
	if err := bs.root.MkdirAll(shardRel, 0o755); err != nil {
		t.Fatal(err)
	}
	shardAbs := filepath.Join(dir, shardRel)
	if err := os.Chmod(shardAbs, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(shardAbs, 0o755) })
	err = bs.commit(s)
	if err == nil {
		t.Fatal("expected rename error")
	}
}

func TestBlobStoreRemoveMissingIsNoOp(t *testing.T) {
	_, bs := newBlobStoreForTest(t)
	sum := strings.Repeat("0", 64)
	if err := bs.remove(sum); err != nil {
		t.Fatal(err)
	}
}

func TestBlobStoreRemoveError(t *testing.T) {
	dir, bs := newBlobStoreForTest(t)
	sum := strings.Repeat("a", 64)
	blobRel := blobPath(sum)
	if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(blobRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, blobRel), 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(blobRel, "child")
	if err := os.WriteFile(filepath.Join(dir, child), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := bs.remove(sum); err == nil {
		t.Fatal("expected remove error")
	}
}

func TestBlobStoreSweepNoDir(t *testing.T) {
	_, bs := newBlobStoreForTest(t)
	if err := bs.sweep(); err != nil {
		t.Fatal(err)
	}
}

func TestBlobStoreSweepReadDirError(t *testing.T) {
	dir, bs := newBlobStoreForTest(t)
	if err := os.MkdirAll(filepath.Join(dir, sessionartifacts.VersionsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, blobsDir), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := bs.sweep(); err == nil {
		t.Fatal("expected readdir error")
	}
}

func TestBlobStoreSweepRemovesIncoming(t *testing.T) {
	dir, bs := newBlobStoreForTest(t)
	if err := os.MkdirAll(filepath.Join(dir, blobsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	incomingRel := filepath.Join(blobsDir, incomingPrefix+"dead")
	if err := os.WriteFile(filepath.Join(dir, incomingRel), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	keepRel := filepath.Join(blobsDir, "keep.txt")
	if err := os.WriteFile(filepath.Join(dir, keepRel), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := bs.sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, incomingRel)); !os.IsNotExist(err) {
		t.Fatalf("incoming not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, keepRel)); err != nil {
		t.Fatalf("keep removed: %v", err)
	}
}

func TestBlobStoreSweepRemoveError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission tests require non-root")
	}
	dir, bs := newBlobStoreForTest(t)
	if err := os.MkdirAll(filepath.Join(dir, blobsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	incomingAbs := filepath.Join(dir, blobsDir, incomingPrefix+"dead")
	if err := os.WriteFile(incomingAbs, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	blobsAbs := filepath.Join(dir, blobsDir)
	if err := os.Chmod(blobsAbs, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blobsAbs, 0o755) })
	if err := bs.sweep(); err == nil {
		t.Fatal("expected sweep remove error")
	}
}

func TestBlobStoreDiscardRemovesTemp(t *testing.T) {
	_, bs := newBlobStoreForTest(t)
	s, err := bs.stage(strings.NewReader("x"), 10)
	if err != nil {
		t.Fatal(err)
	}
	bs.discard(s)
	if _, err := bs.root.Stat(s.tmp); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("staged temp not removed: %v", err)
	}
}

// Keep an unused import guard to avoid compile errors if fmt unused.
var _ = fmt.Sprintf

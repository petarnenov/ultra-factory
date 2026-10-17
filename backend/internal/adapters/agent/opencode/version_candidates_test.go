package opencode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func candidateExecutable(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	path := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCandidateSupplierPreservesVersionSelection(t *testing.T) {
	malformed := candidateExecutable(t, "printf 'development\\n'\n")
	v1 := candidateExecutable(t, "printf '1.18.33\\n'\n")
	v2 := candidateExecutable(t, "printf '2.0.0\\n'\n")
	laterV2 := candidateExecutable(t, "printf '2.1.0\\n'\n")
	for _, tc := range []struct {
		name       string
		candidates []string
		want       string
		mismatch   bool
	}{
		{"malformed only", []string{malformed}, "", false},
		{"malformed then mismatch", []string{malformed, v1}, "", true},
		{"mismatch then malformed", []string{v1, malformed}, "", true},
		{"mixed finds v2", []string{malformed, v1, v2}, v2, false},
		{"first matching candidate", []string{malformed, v2, laterV2}, v2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveBinaryForMajorWithCandidates(context.Background(), 2, func(context.Context) ([]string, error) {
				return tc.candidates, nil
			})
			if got != tc.want {
				t.Fatalf("binary = %q, want %q (error %v)", got, tc.want, err)
			}
			if tc.want != "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var mismatch *IncompatibleVersionError
			if tc.mismatch {
				if !errors.As(err, &mismatch) || mismatch.Path != v1 || mismatch.FoundMajor != 1 || mismatch.ExpectedMajor != 2 {
					t.Fatalf("error = %v, want classified v1 mismatch", err)
				}
			} else if err == nil || errors.As(err, &mismatch) || !strings.Contains(err.Error(), "cannot determine version") {
				t.Fatalf("error = %v, want malformed-version error", err)
			}
		})
	}
}

func TestCandidateSupplierErrorsAndEmptyResult(t *testing.T) {
	failure := errors.New("discovery unavailable")
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"discovery failure", failure, failure},
		{"no candidates", nil, ports.ErrAgentBinaryNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveBinaryForMajorWithCandidates(context.Background(), 2, func(context.Context) ([]string, error) {
				return nil, tc.err
			})
			if got != "" || !errors.Is(err, tc.want) {
				t.Fatalf("resolve = (%q, %v), want %v", got, err, tc.want)
			}
		})
	}
}

func TestCandidateDiscoveryAndProbeShareDeadline(t *testing.T) {
	// A short parent deadline alone cannot detect resetting the internal
	// timeout from that same parent. Keep the real default-budget case too.
	for _, tc := range []struct {
		name           string
		parentBudget   time.Duration
		discoveryDelay time.Duration
	}{
		{"parent deadline", time.Second, 500 * time.Millisecond},
		{"default shared budget", 0, versionProbeTimeout - 2*time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "probe-pid")
			t.Setenv("AO_TEST_PROBE_MARKER", marker)
			// exec leaves no descendant holding output after cancellation. The
			// marker proves resolution reached the real executable, rather than
			// returning solely because discovery consumed the whole deadline.
			binary := candidateExecutable(t, "printf '%s' \"$$\" > \"$AO_TEST_PROBE_MARKER\"\nexec /bin/sleep 3\n")
			parent := context.Background()
			if tc.parentBudget != 0 {
				var cancel context.CancelFunc
				parent, cancel = context.WithTimeout(parent, tc.parentBudget)
				defer cancel()
			}
			var discoveryContext context.Context
			got, err := ResolveBinaryForMajorWithCandidates(parent, 2, func(ctx context.Context) ([]string, error) {
				discoveryContext = ctx
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("discovery has no deadline")
				}
				if parentDeadline, ok := parent.Deadline(); ok && !deadline.Equal(parentDeadline) {
					t.Fatalf("discovery deadline %v does not inherit parent deadline %v", deadline, parentDeadline)
				}
				timer := time.NewTimer(tc.discoveryDelay)
				defer timer.Stop()
				select {
				case <-timer.C:
					return []string{binary}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			})
			if got != "" || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("resolve = (%q, %v), want shared deadline exhaustion", got, err)
			}
			if discoveryContext == nil || !errors.Is(discoveryContext.Err(), context.DeadlineExceeded) {
				t.Fatal("probe did not finish with discovery's deadline exhausted")
			}
			if tc.parentBudget != 0 && !errors.Is(parent.Err(), context.DeadlineExceeded) {
				t.Fatal("resolution did not preserve parent deadline cancellation")
			}
			data, err := os.ReadFile(marker)
			if err != nil {
				t.Fatalf("real version probe was not reached: %v", err)
			}
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatalf("invalid fixture pid: %v", err)
			}
			process, err := os.FindProcess(pid)
			if err != nil {
				t.Fatal(err)
			}
			defer process.Release()
			if err := process.Signal(syscall.Signal(0)); err == nil {
				_ = process.Kill()
				t.Fatal("real probe survived resolution's deadline cancellation")
			}
		})
	}
}

func TestCandidateSupplierReceivesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	got, err := ResolveBinaryForMajorWithCandidates(ctx, 2, func(ctx context.Context) ([]string, error) {
		called = true
		return nil, ctx.Err()
	})
	if !called || got != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("resolve = (%q, %v), called=%v; want cancellation", got, err, called)
	}
}

func TestNilCandidateSupplierUsesRealDiscovery(t *testing.T) {
	binary := candidateExecutable(t, "printf '2.0.0\\n'\n")
	t.Setenv("PATH", filepath.Dir(binary))
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, resolve := range []func(context.Context, int) (string, error){
		ResolveBinaryForMajor,
		func(ctx context.Context, major int) (string, error) {
			return ResolveBinaryForMajorWithCandidates(ctx, major, nil)
		},
	} {
		got, err := resolve(context.Background(), 2)
		if err != nil || got != binary {
			t.Fatalf("default discovery = (%q, %v), want PATH fixture %q", got, err, binary)
		}
	}
}

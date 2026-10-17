package opencode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	binary := candidateExecutable(t, "/bin/sleep 1\nprintf '2.0.0\\n'\n")
	got, err := ResolveBinaryForMajorWithCandidates(context.Background(), 2, func(ctx context.Context) ([]string, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > versionProbeTimeout {
			t.Fatal("discovery did not receive the bounded probe context")
		}
		// Spend the discovery budget before probing a real executable. Giving
		// the probe a new deadline would incorrectly let this binary succeed.
		timer := time.NewTimer(time.Until(deadline) - 50*time.Millisecond)
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

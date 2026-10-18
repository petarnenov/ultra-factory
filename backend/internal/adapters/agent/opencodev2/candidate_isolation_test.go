package opencodev2

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/opencode"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestV2CandidateSupplierUsedByEveryEntryPoint(t *testing.T) {
	binary := fakeBinary(t, "bad")
	dataRoot := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(dataRoot) {
		t.Fatal("fixture data root must be absolute")
	}
	// A valid fixture on PATH must not rescue the injected malformed binary.
	pathBinary := fakeBinary(t, "2.0.0")
	t.Setenv("XDG_DATA_HOME", dataRoot)
	t.Setenv("OPENCODE_CONFIG_CONTENT", "")
	calls := 0
	plugin := &Plugin{binaryCandidates: func(ctx context.Context) ([]string, error) {
		calls++
		return []string{binary}, ctx.Err()
	}}
	for _, entry := range []struct {
		name string
		run  func() error
	}{
		{"resolve", func() error {
			got, err := plugin.ResolveBinary(context.Background())
			if got != "" {
				t.Errorf("failed resolution returned %q", got)
			}
			return err
		}},
		{"launch", func() error {
			got, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{})
			if got != nil {
				t.Errorf("failed launch returned %v", got)
			}
			return err
		}},
		{"restore", func() error {
			got, ok, err := plugin.GetRestoreCommand(context.Background(), ports.RestoreConfig{Session: ports.SessionRef{Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "ses_fixture"}}})
			if got != nil || ok {
				t.Errorf("failed restore returned %v, %v", got, ok)
			}
			return err
		}},
		{"auth", func() error {
			got, err := plugin.AuthStatus(context.Background())
			if got != ports.AgentAuthStatusUnknown {
				t.Errorf("failed auth returned %q", got)
			}
			return err
		}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			before := calls
			err := entry.run()
			if err == nil || !strings.Contains(err.Error(), "cannot determine version") || !strings.Contains(err.Error(), binary) {
				t.Fatalf("error = %v, want malformed injected binary (PATH contains %q)", err, pathBinary)
			}
			if calls != before+1 {
				t.Fatalf("supplier called %d times, want once", calls-before)
			}
		})
	}
	entries, err := os.ReadDir(dataRoot)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed resolution changed isolated data root: %v, %v", entries, err)
	}
}

func TestV2CandidateSuppliersAreInstanceLocal(t *testing.T) {
	v1 := fakeBinary(t, "1.18.33")
	v2 := fakeBinary(t, "2.0.0")
	first, second := fixturePlugin(v1), fixturePlugin(v2)
	for range 2 {
		if got, err := second.ResolveBinary(context.Background()); err != nil || got != v2 {
			t.Fatalf("second instance = (%q, %v), want %q", got, err, v2)
		}
		got, err := first.ResolveBinary(context.Background())
		var mismatch *opencode.IncompatibleVersionError
		if got != "" || !errors.As(err, &mismatch) || mismatch.Path != v1 {
			t.Fatalf("first instance = (%q, %v), want its own v1 mismatch", got, err)
		}
	}
	for _, plugin := range []*Plugin{New(), {}} {
		if got, err := plugin.ResolveBinary(context.Background()); err != nil || got != v2 {
			t.Fatalf("default instance = (%q, %v), want real PATH discovery %q", got, err, v2)
		}
	}
}

package multi

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type recordingDraftSetter struct{ got []ports.SCMDraftRequest }

func (r *recordingDraftSetter) SetPullRequestDraft(_ context.Context, req ports.SCMDraftRequest) error {
	r.got = append(r.got, req)
	return nil
}

func TestDraftSetterRoutesByProvider(t *testing.T) {
	gh := &recordingDraftSetter{}
	d := NewDraftSetter(NamedDraftSetter{Key: "github", Setter: gh})

	req := ports.SCMDraftRequest{PR: ports.SCMPRRef{Repo: ports.SCMRepo{Provider: "github"}, Number: 1}}
	if err := d.SetPullRequestDraft(context.Background(), req); err != nil || len(gh.got) != 1 {
		t.Fatalf("github: err = %v, calls = %d", err, len(gh.got))
	}
	req.PR.Repo.Provider = "gitlab"
	if err := d.SetPullRequestDraft(context.Background(), req); !errors.Is(err, ports.ErrSCMUnsupported) {
		t.Fatalf("gitlab: err = %v, want ErrSCMUnsupported", err)
	}
}

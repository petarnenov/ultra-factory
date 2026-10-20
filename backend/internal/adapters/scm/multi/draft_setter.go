package multi

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// NamedDraftSetter pairs a provider routing key with its draft setter.
type NamedDraftSetter struct {
	Key    string
	Setter ports.SCMDraftSetter
}

// DraftSetter routes SetPullRequestDraft to the sub-provider matching
// SCMPRRef.Repo.Provider. A provider without a draft setter is unsupported.
type DraftSetter struct {
	byName map[string]ports.SCMDraftSetter
}

// NewDraftSetter creates a DraftSetter from the given sub-providers.
func NewDraftSetter(setters ...NamedDraftSetter) *DraftSetter {
	d := &DraftSetter{byName: make(map[string]ports.SCMDraftSetter, len(setters))}
	for _, ns := range setters {
		d.byName[ns.Key] = ns.Setter
	}
	return d
}

// SetPullRequestDraft delegates to the matching sub-provider.
func (d *DraftSetter) SetPullRequestDraft(ctx context.Context, request ports.SCMDraftRequest) error {
	setter, ok := d.byName[request.PR.Repo.Provider]
	if !ok {
		return fmt.Errorf("%w: draft state for provider %q", ports.ErrSCMUnsupported, request.PR.Repo.Provider)
	}
	return setter.SetPullRequestDraft(ctx, request)
}

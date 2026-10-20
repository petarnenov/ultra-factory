package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var _ ports.SCMDraftSetter = (*Provider)(nil)

const draftLookupQuery = `query PullRequestDraft($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) { pullRequest(number: $number) { id isDraft } }
}`

const markReadyMutation = `mutation MarkReady($id: ID!) {
  markPullRequestReadyForReview(input: { pullRequestId: $id }) { pullRequest { isDraft } }
}`

const convertToDraftMutation = `mutation ConvertToDraft($id: ID!) {
  convertPullRequestToDraft(input: { pullRequestId: $id }) { pullRequest { isDraft } }
}`

// SetPullRequestDraft marks a pull request ready for review or converts it
// back to a draft through the GraphQL API. The flip needs a user or app token;
// it is done with the account token the daemon already uses for merges.
func (p *Provider) SetPullRequestDraft(ctx context.Context, request ports.SCMDraftRequest) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("github scm: draft setter is not configured")
	}
	repo := request.PR.Repo
	if request.PR.Number <= 0 || strings.TrimSpace(repo.Owner) == "" || strings.TrimSpace(repo.Name) == "" {
		return fmt.Errorf("github scm: invalid pull request reference")
	}
	scoped, err := p.withAccountToken(ctx, repo.Host, repo.Account)
	if err != nil {
		return err
	}
	ctx = scoped

	data, err := p.client.doGraphQL(ctx, draftLookupQuery, map[string]any{
		"owner": repo.Owner, "name": repo.Name, "number": request.PR.Number,
	})
	if err != nil {
		return err
	}
	repository, _ := data["repository"].(map[string]any)
	pull, _ := repository["pullRequest"].(map[string]any)
	id, _ := pull["id"].(string)
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: pull request #%d", ports.ErrSCMNotFound, request.PR.Number)
	}
	if current, ok := pull["isDraft"].(bool); ok && current == request.Draft {
		return nil
	}

	query, field := markReadyMutation, "markPullRequestReadyForReview"
	if request.Draft {
		query, field = convertToDraftMutation, "convertPullRequestToDraft"
	}
	data, err = p.client.doGraphQL(ctx, query, map[string]any{"id": id})
	if err != nil {
		return err
	}
	mutation, _ := data[field].(map[string]any)
	updated, _ := mutation["pullRequest"].(map[string]any)
	isDraft, ok := updated["isDraft"].(bool)
	if !ok {
		return errors.New("github scm: draft mutation returned no pull request")
	}
	if isDraft != request.Draft {
		return fmt.Errorf("github scm: draft state was not changed to %t", request.Draft)
	}
	return nil
}

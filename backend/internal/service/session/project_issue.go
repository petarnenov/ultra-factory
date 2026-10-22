package session

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

// ProjectIssue reads issue number from the project's tracker. The provider
// comes from the project's SCM origin, as for spawn --issue.
func (s *Service) ProjectIssue(ctx context.Context, projectID domain.ProjectID, number int) (domain.Issue, error) {
	if number < 1 {
		return domain.Issue{}, apierr.Invalid("INVALID_ISSUE_NUMBER", "Issue number must be at least 1", nil)
	}
	if s.tracker == nil {
		return domain.Issue{}, apierr.Unavailable("TRACKER_UNAVAILABLE", "No issue tracker is configured")
	}
	project, err := s.requireProject(ctx, projectID)
	if err != nil {
		return domain.Issue{}, err
	}
	provider, host, repo, ok := s.repoForTracker(project, "")
	if !ok {
		return domain.Issue{}, apierr.Invalid("PROJECT_REPO_UNKNOWN", "The project's origin does not identify a repository", nil)
	}
	return s.tracker.Get(ctx, domain.TrackerID{Provider: provider, Native: fmt.Sprintf("%s#%d", repo, number), Host: host})
}

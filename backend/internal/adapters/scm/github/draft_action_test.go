package github

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func draftRequest(draft bool) ports.SCMDraftRequest {
	return ports.SCMDraftRequest{PR: validMergeRequest().PR, Draft: draft}
}

// graphqlScript answers the lookup query with the PR's current draft state and
// records the mutations it receives.
func graphqlScript(t *testing.T, f *fakeGH, currentDraft bool, mutationDraft bool) *[]string {
	t.Helper()
	var mutations []string
	f.on(http.MethodPost, "/graphql", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.Contains(body.Query, "PullRequestDraft"):
			if body.Variables["owner"] != "octocat" || body.Variables["name"] != "hello" || body.Variables["number"] != float64(42) {
				t.Fatalf("lookup variables = %#v", body.Variables)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{
				"pullRequest": map[string]any{"id": "PR_node", "isDraft": currentDraft},
			}}})
		case strings.Contains(body.Query, "markPullRequestReadyForReview"):
			mutations = append(mutations, "ready:"+body.Variables["id"].(string))
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"markPullRequestReadyForReview": map[string]any{
				"pullRequest": map[string]any{"isDraft": mutationDraft},
			}}})
		case strings.Contains(body.Query, "convertPullRequestToDraft"):
			mutations = append(mutations, "draft:"+body.Variables["id"].(string))
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"convertPullRequestToDraft": map[string]any{
				"pullRequest": map[string]any{"isDraft": mutationDraft},
			}}})
		default:
			t.Fatalf("unexpected query %q", body.Query)
		}
	})
	return &mutations
}

func TestSetPullRequestDraft_MarksReadyAndConvertsToDraft(t *testing.T) {
	f := newFakeGH(t)
	mutations := graphqlScript(t, f, true, false)
	if err := newProviderForTest(t, f).SetPullRequestDraft(ctx(), draftRequest(false)); err != nil {
		t.Fatal(err)
	}
	if len(*mutations) != 1 || (*mutations)[0] != "ready:PR_node" {
		t.Fatalf("mutations = %v", *mutations)
	}

	f = newFakeGH(t)
	mutations = graphqlScript(t, f, false, true)
	if err := newProviderForTest(t, f).SetPullRequestDraft(ctx(), draftRequest(true)); err != nil {
		t.Fatal(err)
	}
	if len(*mutations) != 1 || (*mutations)[0] != "draft:PR_node" {
		t.Fatalf("mutations = %v", *mutations)
	}
}

func TestSetPullRequestDraft_AlreadyInStateSkipsMutation(t *testing.T) {
	f := newFakeGH(t)
	mutations := graphqlScript(t, f, false, false)
	if err := newProviderForTest(t, f).SetPullRequestDraft(ctx(), draftRequest(false)); err != nil {
		t.Fatal(err)
	}
	if len(*mutations) != 0 {
		t.Fatalf("mutations = %v, want none", *mutations)
	}
}

func TestSetPullRequestDraft_RejectsUnconfirmedChangeAndMissingPR(t *testing.T) {
	f := newFakeGH(t)
	graphqlScript(t, f, true, true)
	if err := newProviderForTest(t, f).SetPullRequestDraft(ctx(), draftRequest(false)); err == nil {
		t.Fatal("an unconfirmed flip succeeded")
	}

	f = newFakeGH(t)
	f.on(http.MethodPost, "/graphql", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": nil}}})
	})
	err := newProviderForTest(t, f).SetPullRequestDraft(ctx(), draftRequest(false))
	if !errors.Is(err, ports.ErrSCMNotFound) {
		t.Fatalf("error = %v, want ErrSCMNotFound", err)
	}
}

func TestSetPullRequestDraft_RejectsInvalidReference(t *testing.T) {
	request := draftRequest(false)
	request.PR.Number = 0
	if err := newProviderForTest(t, newFakeGH(t)).SetPullRequestDraft(ctx(), request); err == nil {
		t.Fatal("invalid reference succeeded")
	}
}

package controllers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
)

// pageBudgetPerActivity is the byte budget per file_change in a page.
const pageBudgetPerActivity = 20 * 1024

// bigFile is a large file; a one-line edit has a known diff.
func bigFile(lines int, edited string) string {
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		if i == lines/2 && edited != "" {
			b.WriteString(edited + "\n")
			continue
		}
		b.WriteString("line of ordinary source text number " + strings.Repeat("x", 40) + "\n")
	}
	return b.String()
}

func acpFileChange(t *testing.T, id string, seq int64, oldText, newText string) domain.ConversationActivity {
	t.Helper()
	detail, err := json.Marshal(map[string]any{
		"protocol": "acp",
		"files": []any{map[string]any{
			"path": "pkg/big.go", "status": "modified", "additions": 1, "deletions": 1,
			"patch":   "--- pkg/big.go\n+++ pkg/big.go\n-" + oldText + "\n+" + newText,
			"oldText": oldText, "newText": newText,
		}},
		"content": []any{map[string]any{"type": "diff", "path": "pkg/big.go", "oldText": oldText, "newText": newText}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return domain.ConversationActivity{
		ID: id, Sequence: seq, Kind: domain.ActivityKindFileChange, Status: domain.ActivityStatusCompleted,
		Summary: "Edited pkg/big.go (1 replacement)", Detail: detail,
		CreatedAt: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
	}
}

func TestConversationPageBoundsFileChangeDetail(t *testing.T) {
	oldText := bigFile(20000, "")
	newText := bigFile(20000, "the one edited line")
	if len(oldText) < 1_000_000 {
		t.Fatalf("fixture too small: %d bytes", len(oldText))
	}
	var activities []domain.ConversationActivity
	for i := range 5 {
		activities = append(activities, acpFileChange(t, "a"+string(rune('0'+i)), int64(i+1), oldText, newText))
	}
	server := conversationTestServer(t, &fakeConversationService{snapshot: chatsvc.Snapshot{
		Conversation: domain.ConversationRecord{ID: "c1"}, SessionID: "p1-1", Activities: activities,
	}})
	resp, err := http.Get(server.URL + "/api/v1/sessions/p1-1/conversation")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if len(raw) > len(activities)*pageBudgetPerActivity {
		t.Fatalf("page is %d bytes for %d activities, budget is %d each", len(raw), len(activities), pageBudgetPerActivity)
	}
	var body struct {
		Activities []struct {
			Detail map[string]any `json:"detail"`
		} `json:"activities"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	detail := body.Activities[0].Detail
	if detail["detailTruncated"] != true {
		t.Fatalf("detailTruncated = %#v, want true", detail["detailTruncated"])
	}
	file := detail["files"].([]any)[0].(map[string]any)
	if _, ok := file["oldText"]; ok {
		t.Fatal("oldText must not travel in a page")
	}
	patch := file["patch"].(string)
	if !strings.Contains(patch, "+the one edited line") || !strings.Contains(patch, "@@ ") {
		t.Fatalf("patch lost the edit or its hunk header:\n%s", patch)
	}
	if file["additions"] != float64(1) || file["path"] != "pkg/big.go" {
		t.Fatalf("file stats were not preserved: %#v", file)
	}
}

func TestCompactPatchShowsOnlyTheChangedHunk(t *testing.T) {
	oldText := "a\nb\nc\nd\ne\nf\ng\nh\ni\n"
	newText := "a\nb\nc\nd\nE\nf\ng\nh\ni\n"
	detail := map[string]any{"files": []any{map[string]any{"path": "x", "oldText": oldText, "newText": newText}}}
	body := conversationSnapshotBody(t, chatsvc.Snapshot{
		Conversation: domain.ConversationRecord{ID: "c1"}, SessionID: "p1-1",
		Activities: []domain.ConversationActivity{{
			ID: "a", Sequence: 1, Kind: domain.ActivityKindFileChange, Status: domain.ActivityStatusCompleted,
			Detail: detailJSON(t, detail),
		}},
	})
	file := body["activities"].([]any)[0].(map[string]any)["detail"].(map[string]any)["files"].([]any)[0].(map[string]any)
	want := "--- x\n+++ x\n@@ -2,7 +2,7 @@\n b\n c\n d\n-e\n+E\n f\n g\n h"
	if file["patch"] != want {
		t.Fatalf("patch = %q\nwant    %q", file["patch"], want)
	}
}

func TestOversizedPatchIsCutAndFlagged(t *testing.T) {
	detail := map[string]any{"files": []any{map[string]any{"path": "x", "patch": strings.Repeat("+é line\n", 20000)}}}
	body := conversationSnapshotBody(t, chatsvc.Snapshot{
		Conversation: domain.ConversationRecord{ID: "c1"}, SessionID: "p1-1",
		Activities: []domain.ConversationActivity{{
			ID: "a", Sequence: 1, Kind: domain.ActivityKindFileChange, Detail: detailJSON(t, detail),
		}},
	})
	d := body["activities"].([]any)[0].(map[string]any)["detail"].(map[string]any)
	file := d["files"].([]any)[0].(map[string]any)
	if file["patchTruncated"] != true || d["detailTruncated"] != true {
		t.Fatalf("truncation not flagged: %#v", d)
	}
	if n := len(file["patch"].(string)); n > 16*1024 {
		t.Fatalf("patch is %d bytes", n)
	}
}

func TestNonFileChangeDetailIsNotBounded(t *testing.T) {
	body := conversationSnapshotBody(t, chatsvc.Snapshot{
		Conversation: domain.ConversationRecord{ID: "c1"}, SessionID: "p1-1",
		Activities: []domain.ConversationActivity{{
			ID: "a", Sequence: 1, Kind: domain.ActivityKindCommand,
			Detail: detailJSON(t, map[string]any{"oldText": "kept"}),
		}},
	})
	d := body["activities"].([]any)[0].(map[string]any)["detail"].(map[string]any)
	if d["oldText"] != "kept" || d["detailTruncated"] != nil {
		t.Fatalf("command detail was altered: %#v", d)
	}
}

type activityService struct {
	*fakeConversationService
	activity domain.ConversationActivity
	err      error
}

func (a *activityService) Activity(_ context.Context, _ domain.SessionID, id string) (domain.ConversationActivity, error) {
	if a.err != nil || id != a.activity.ID {
		if a.err == nil {
			return domain.ConversationActivity{}, domain.ErrNoConversationActivity
		}
		return domain.ConversationActivity{}, a.err
	}
	return a.activity, nil
}

func TestActivityEndpointReturnsTheFullDetail(t *testing.T) {
	activity := acpFileChange(t, "a1", 1, "old text", "new text")
	server := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, httpd.APIDeps{
		Sessions:      newFakeSessionService(),
		Conversations: &activityService{fakeConversationService: &fakeConversationService{}, activity: activity},
	}, httpd.ControlDeps{}))
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/api/v1/sessions/p1-1/conversation/activities/a1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got struct {
		ID     string         `json:"id"`
		Detail map[string]any `json:"detail"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	file := got.Detail["files"].([]any)[0].(map[string]any)
	if got.ID != "a1" || file["oldText"] != "old text" || file["newText"] != "new text" || got.Detail["detailTruncated"] != nil {
		t.Fatalf("full detail not returned: %#v", got)
	}

	missing, err := http.Get(server.URL + "/api/v1/sessions/p1-1/conversation/activities/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = missing.Body.Close() }()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown activity status = %d, want 404", missing.StatusCode)
	}
}

func detailJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

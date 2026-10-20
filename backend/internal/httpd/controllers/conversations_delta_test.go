package controllers_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"
)

func deltaSnapshot(base time.Time) chatsvc.Snapshot {
	return chatsvc.Snapshot{
		Conversation: domain.ConversationRecord{ID: "conversation-1", UpdatedAt: base},
		SessionID:    domain.SessionID("p1-1"),
		Turns:        []domain.ConversationTurn{{ID: "t1", RequestedAt: base}},
		Messages: []domain.ConversationMessage{
			{ID: "m1", TurnID: "t1", Sequence: 1, Role: domain.MessageRoleUser, Text: "hi", CreatedAt: base, UpdatedAt: base},
			{ID: "m2", TurnID: "t1", Sequence: 3, Role: domain.MessageRoleAssistant, Text: "hello", CreatedAt: base, UpdatedAt: base.Add(2 * time.Second)},
		},
		Activities: []domain.ConversationActivity{
			{ID: "a1", TurnID: "t1", Sequence: 2, Kind: domain.ActivityKindCommand, Status: domain.ActivityStatusCompleted, CreatedAt: base, UpdatedAt: base.Add(time.Second)},
		},
	}
}

func getConversation(t *testing.T, snapshot chatsvc.Snapshot, query string, header http.Header) (*http.Response, map[string]any) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		Sessions:      newFakeSessionService(),
		Conversations: &fakeConversationService{snapshot: snapshot},
	}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/sessions/p1-1/conversation"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode %q: %v", raw, err)
		}
	}
	return resp, decoded
}

func ids(body map[string]any, key string) []string {
	var out []string
	for _, item := range body[key].([]any) {
		out = append(out, item.(map[string]any)["id"].(string))
	}
	return out
}

func TestConversationWithoutCursorIsFullAndCarriesRevision(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	_, body := getConversation(t, deltaSnapshot(base), "", nil)
	if got := fmt.Sprint(ids(body, "messages")); got != "[m1 m2]" {
		t.Fatalf("messages = %s, want the full page", got)
	}
	if body["delta"] != nil {
		t.Fatalf("a request without a cursor must not be marked delta: %v", body["delta"])
	}
	want := float64(base.Add(2 * time.Second).UnixNano())
	if body["revision"] != want {
		t.Fatalf("revision = %v, want the newest write time %v", body["revision"], want)
	}
}

func TestConversationCursorReturnsOnlyChangedEntries(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	since := base.Add(2 * time.Second).UnixNano()
	_, body := getConversation(t, deltaSnapshot(base), fmt.Sprintf("?sinceRevision=%d", since), nil)
	if body["delta"] != true {
		t.Fatalf("delta = %v, want true", body["delta"])
	}
	if got := fmt.Sprint(ids(body, "messages"), ids(body, "activities"), ids(body, "turns")); got != "[m2] [] [t1]" {
		t.Fatalf("delta = %s, want only m2", got)
	}
	if body["latestSequence"] == nil || body["conversationId"] != "conversation-1" {
		t.Fatalf("delta must keep the conversation metadata: %v", body)
	}
}

func TestConversationCursorBehindReturnsEverythingNewer(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	since := base.Add(time.Second).UnixNano()
	_, body := getConversation(t, deltaSnapshot(base), fmt.Sprintf("?sinceRevision=%d", since), nil)
	if got := fmt.Sprint(ids(body, "messages"), ids(body, "activities")); got != "[m2] [a1]" {
		t.Fatalf("delta = %s, want m2 and a1", got)
	}
}

func TestConversationCursorOnWindowedPageFallsBackWhenOldestChanged(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	snapshot := deltaSnapshot(base)
	snapshot.HasMoreBefore, snapshot.OldestSequence = true, 1
	_, body := getConversation(t, snapshot, fmt.Sprintf("?sinceRevision=%d", base.UnixNano()), nil)
	if body["delta"] != nil || fmt.Sprint(ids(body, "messages")) != "[m1 m2]" {
		t.Fatalf("an oldest-entry change on a windowed page must serve the full page: %v", body)
	}
}

func TestConversationCursorAheadOfHostIsRejected(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	resp, body := getConversation(t, deltaSnapshot(base), fmt.Sprintf("?sinceRevision=%d", base.Add(time.Hour).UnixNano()), nil)
	if resp.StatusCode != http.StatusConflict || body["code"] != "CONVERSATION_CURSOR_AHEAD" {
		t.Fatalf("status = %d body = %v, want 409 CONVERSATION_CURSOR_AHEAD", resp.StatusCode, body)
	}
}

func TestConversationCursorValidation(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, query := range []string{"?sinceRevision=abc", "?sinceRevision=0", "?sinceRevision=5&beforeSequence=3"} {
		resp, body := getConversation(t, deltaSnapshot(base), query, nil)
		if resp.StatusCode != http.StatusBadRequest || body["code"] != "CONVERSATION_CURSOR_INVALID" {
			t.Fatalf("%s: status = %d body = %v, want 400 CONVERSATION_CURSOR_INVALID", query, resp.StatusCode, body)
		}
	}
}

func TestConversationUnchangedAnswersNotModified(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, query := range []string{"", fmt.Sprintf("?sinceRevision=%d", base.Add(2*time.Second).UnixNano())} {
		first, _ := getConversation(t, deltaSnapshot(base), query, nil)
		etag := first.Header.Get("ETag")
		if etag == "" {
			t.Fatalf("%q: first response carries no ETag", query)
		}
		resp, body := getConversation(t, deltaSnapshot(base), query, http.Header{"If-None-Match": {etag}})
		if resp.StatusCode != http.StatusNotModified || body != nil {
			t.Fatalf("%q: status = %d body = %v, want an empty 304", query, resp.StatusCode, body)
		}
		changed := deltaSnapshot(base)
		changed.Messages[1].Text = "edited"
		changed.Messages[1].UpdatedAt = base.Add(3 * time.Second)
		resp, _ = getConversation(t, changed, query, http.Header{"If-None-Match": {etag}})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%q: a changed conversation answered %d, want 200", query, resp.StatusCode)
		}
	}
}

func TestConversationIfNoneMatchUsesWeakComparison(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	first, _ := getConversation(t, deltaSnapshot(base), "", nil)
	etag := first.Header.Get("ETag")
	strong := etag[len("W/"):]
	for _, header := range []string{"*", strong, `"other", ` + etag} {
		resp, _ := getConversation(t, deltaSnapshot(base), "", http.Header{"If-None-Match": {header}})
		if resp.StatusCode != http.StatusNotModified {
			t.Fatalf("If-None-Match %q answered %d, want 304", header, resp.StatusCode)
		}
	}
	resp, _ := getConversation(t, deltaSnapshot(base), "", http.Header{"If-None-Match": {`"other"`}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a different tag answered %d, want 200", resp.StatusCode)
	}
}

func TestReviewerConversationHonoursCursorAndETag(t *testing.T) {
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	service := &fakeConversationService{reviewSnapshot: deltaSnapshot(base)}
	server := conversationTestServer(t, service)
	url := server.URL + "/api/v1/reviews/review-1/conversation"

	status, body := reviewConversationRequest(t, server.Client(), http.MethodGet, fmt.Sprintf("%s?sinceRevision=%d", url, base.Add(2*time.Second).UnixNano()), "")
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil || status != http.StatusOK {
		t.Fatalf("status %d body %s err %v", status, body, err)
	}
	if decoded["delta"] != true || fmt.Sprint(ids(decoded, "messages")) != "[m2]" {
		t.Fatalf("reviewer delta = %s, want only m2", body)
	}

	req, _ := http.NewRequest(http.MethodGet, url, nil)
	first, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Body.Close()
	req, _ = http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("If-None-Match", first.Header.Get("ETag"))
	again, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = again.Body.Close()
	if again.StatusCode != http.StatusNotModified {
		t.Fatalf("reviewer conditional GET = %d, want 304", again.StatusCode)
	}
}

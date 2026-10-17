package chat

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestClientPayloadHashPreservesLegacyEmptySenderHash(t *testing.T) {
	msg := ports.ChatUserMessage{ClientMessageID: "c1", Text: "hi"}
	got, err := clientPayloadHash(msg)
	if err != nil {
		t.Fatalf("clientPayloadHash: %v", err)
	}

	legacyPayload, err := json.Marshal(struct {
		Text           string
		Content        []ports.ChatContent
		Origin         domain.MessageOrigin
		AuthoredByUser bool
		Settings       ports.ChatTurnSettings
	}{msg.Text, nil, normalizeOrigin(msg.Origin), msg.AuthoredByUser, msg.Settings})
	if err != nil {
		t.Fatalf("marshal legacy payload: %v", err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(legacyPayload))
	if got != want {
		t.Fatalf("empty sender hash = %s, want legacy %s", got, want)
	}

	withSender, err := clientPayloadHash(ports.ChatUserMessage{
		ClientMessageID: "c1", Text: "hi", SenderSessionID: "worker-1",
	})
	if err != nil {
		t.Fatalf("clientPayloadHash with sender: %v", err)
	}
	if withSender == got {
		t.Fatal("sender identity did not change the payload hash")
	}
}

func mustClientPayloadHash(t *testing.T, msg ports.ChatUserMessage) string {
	t.Helper()
	got, err := clientPayloadHash(msg)
	if err != nil {
		t.Fatalf("clientPayloadHash: %v", err)
	}
	return got
}

func assertClientPayloadHashChanges(t *testing.T, base ports.ChatUserMessage, baseHash, name string, mutate func(*ports.ChatUserMessage)) {
	t.Helper()
	msg := base
	msg.Content = make([]ports.ChatContent, len(base.Content))
	copy(msg.Content, base.Content)
	for i := range msg.Content {
		if msg.Content[i].Excerpt != nil {
			excerpt := *msg.Content[i].Excerpt
			msg.Content[i].Excerpt = &excerpt
		}
	}
	msg.Excerpts = append([]ports.ChatExcerptReference(nil), base.Excerpts...)
	mutate(&msg)
	got := mustClientPayloadHash(t, msg)
	if got == baseHash {
		t.Fatalf("%s: expected hash to change, but it stayed %s", name, got)
	}
}

func TestClientPayloadHashBehaviorAndBoundaries(t *testing.T) {
	t.Parallel()

	base := ports.ChatUserMessage{
		ClientMessageID: "c1",
		Text:            "hi",
		Content: []ports.ChatContent{{
			Type:     "text",
			Data:     "data",
			MIMEType: "text/plain",
			URI:      "file:///tmp/x",
			Name:     "x",
			Text:     "context",
			Internal: true,
		}},
		Excerpts: []ports.ChatExcerptReference{{
			ConversationID: "conv-1",
			MessageID:      "msg-1",
			Revision:       7,
			Text:           "selected",
		}},
		Origin:          domain.MessageOriginHuman,
		SenderSessionID: "sender-1",
		AuthoredByUser:  true,
		Settings: ports.ChatTurnSettings{
			Model:    "model-a",
			Effort:   "high",
			Approval: domain.PermissionModeAuto,
		},
	}
	baseHash := mustClientPayloadHash(t, base)
	if len(baseHash) != 64 {
		t.Fatalf("hash length = %d, want 64", len(baseHash))
	}
	for _, r := range baseHash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			t.Fatalf("hash contains non-hex character %q: %s", r, baseHash)
		}
	}

	t.Run("empty client message id yields no hash", func(t *testing.T) {
		msg := base
		msg.ClientMessageID = ""
		got, err := clientPayloadHash(msg)
		if err != nil {
			t.Fatalf("clientPayloadHash: %v", err)
		}
		if got != "" {
			t.Fatalf("empty client id hash = %q, want empty", got)
		}
	})

	t.Run("same message hashes deterministically", func(t *testing.T) {
		if got := mustClientPayloadHash(t, base); got != baseHash {
			t.Fatalf("hash not deterministic: got %s, want %s", got, baseHash)
		}
	})

	t.Run("client message id alone does not alter payload hash", func(t *testing.T) {
		msg := base
		msg.ClientMessageID = "a-different-client-id"
		if got := mustClientPayloadHash(t, msg); got != baseHash {
			t.Fatalf("client id changed hash: got %s, want %s", got, baseHash)
		}
	})

	t.Run("text change is detected", func(t *testing.T) {
		assertClientPayloadHashChanges(t, base, baseHash, "text", func(m *ports.ChatUserMessage) {
			m.Text = "bye"
		})
	})

	t.Run("content data change is detected", func(t *testing.T) {
		assertClientPayloadHashChanges(t, base, baseHash, "content data", func(m *ports.ChatUserMessage) {
			m.Content[0].Data = "other"
		})
	})

	t.Run("content kind change is detected", func(t *testing.T) {
		assertClientPayloadHashChanges(t, base, baseHash, "content type", func(m *ports.ChatUserMessage) {
			m.Content[0].Type = "image"
		})
	})

	t.Run("content internal flag change is detected", func(t *testing.T) {
		assertClientPayloadHashChanges(t, base, baseHash, "content internal", func(m *ports.ChatUserMessage) {
			m.Content[0].Internal = false
		})
	})

	t.Run("excerpt change is detected", func(t *testing.T) {
		assertClientPayloadHashChanges(t, base, baseHash, "excerpt", func(m *ports.ChatUserMessage) {
			m.Excerpts[0].Text = "other selected"
		})
	})

	t.Run("origin change is detected", func(t *testing.T) {
		assertClientPayloadHashChanges(t, base, baseHash, "origin", func(m *ports.ChatUserMessage) {
			m.Origin = domain.MessageOriginAutomation
		})
	})

	t.Run("authored-by-user change is detected", func(t *testing.T) {
		assertClientPayloadHashChanges(t, base, baseHash, "authored by user", func(m *ports.ChatUserMessage) {
			m.AuthoredByUser = false
		})
	})

	t.Run("sender identity change is detected", func(t *testing.T) {
		assertClientPayloadHashChanges(t, base, baseHash, "sender", func(m *ports.ChatUserMessage) {
			m.SenderSessionID = "sender-2"
		})
	})

	t.Run("missing sender identity changes a populated sender hash", func(t *testing.T) {
		assertClientPayloadHashChanges(t, base, baseHash, "missing sender", func(m *ports.ChatUserMessage) {
			m.SenderSessionID = ""
		})
	})

	t.Run("settings change is detected", func(t *testing.T) {
		assertClientPayloadHashChanges(t, base, baseHash, "settings", func(m *ports.ChatUserMessage) {
			m.Settings.Model = "model-b"
		})
	})

	t.Run("empty content canonicalizes across nil and empty slices", func(t *testing.T) {
		emptyNil := base
		emptyNil.Content = nil
		emptySlice := base
		emptySlice.Content = []ports.ChatContent{}
		hashNil := mustClientPayloadHash(t, emptyNil)
		hashSlice := mustClientPayloadHash(t, emptySlice)
		if hashNil != hashSlice {
			t.Fatalf("empty content hashes differ: nil=%s slice=%s", hashNil, hashSlice)
		}
	})

	t.Run("empty excerpts canonicalize across nil and empty slices", func(t *testing.T) {
		noExcerptsNil := base
		noExcerptsNil.Excerpts = nil
		noExcerptsSlice := base
		noExcerptsSlice.Excerpts = []ports.ChatExcerptReference{}
		if gotNil, gotSlice := mustClientPayloadHash(t, noExcerptsNil), mustClientPayloadHash(t, noExcerptsSlice); gotNil != gotSlice {
			t.Fatalf("empty excerpts hashes differ: nil=%s slice=%s", gotNil, gotSlice)
		}
	})
}

func TestLegacyMessageMatchesBehaviorAndBoundaries(t *testing.T) {
	t.Parallel()

	content := []ports.ChatContent{{Type: "text", Text: "context"}}
	encodedContent, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	encodedOther, err := json.Marshal([]ports.ChatContent{{Type: "text", Text: "other"}})
	if err != nil {
		t.Fatalf("marshal other content: %v", err)
	}

	cases := []struct {
		name     string
		existing domain.ConversationMessage
		msg      ports.ChatUserMessage
		want     bool
	}{
		{
			name:     "matching text, origin, and empty content",
			existing: domain.ConversationMessage{Text: "hi", Origin: domain.MessageOriginHuman, DeliveryContentJSON: ""},
			msg:      ports.ChatUserMessage{Text: "hi", Origin: domain.MessageOriginHuman},
			want:     true,
		},
		{
			name:     "mismatched text",
			existing: domain.ConversationMessage{Text: "hi", Origin: domain.MessageOriginHuman, DeliveryContentJSON: ""},
			msg:      ports.ChatUserMessage{Text: "bye", Origin: domain.MessageOriginHuman},
			want:     false,
		},
		{
			name:     "mismatched origin",
			existing: domain.ConversationMessage{Text: "hi", Origin: domain.MessageOriginHuman, DeliveryContentJSON: ""},
			msg:      ports.ChatUserMessage{Text: "hi", Origin: domain.MessageOriginAutomation},
			want:     false,
		},
		{
			name:     "unspecified inbound origin normalizes to human",
			existing: domain.ConversationMessage{Text: "hi", Origin: domain.MessageOriginHuman, DeliveryContentJSON: ""},
			msg:      ports.ChatUserMessage{Text: "hi", Origin: ""},
			want:     true,
		},
		{
			name:     "empty inbound content requires empty durable content",
			existing: domain.ConversationMessage{Text: "hi", Origin: domain.MessageOriginHuman, DeliveryContentJSON: "null"},
			msg:      ports.ChatUserMessage{Text: "hi", Origin: domain.MessageOriginHuman},
			want:     false,
		},
		{
			name:     "matching nonempty content",
			existing: domain.ConversationMessage{Text: "hi", Origin: domain.MessageOriginHuman, DeliveryContentJSON: string(encodedContent)},
			msg:      ports.ChatUserMessage{Text: "hi", Origin: domain.MessageOriginHuman, Content: content},
			want:     true,
		},
		{
			name:     "mismatched nonempty content",
			existing: domain.ConversationMessage{Text: "hi", Origin: domain.MessageOriginHuman, DeliveryContentJSON: string(encodedContent)},
			msg:      ports.ChatUserMessage{Text: "hi", Origin: domain.MessageOriginHuman, Content: []ports.ChatContent{{Type: "text", Text: "other"}}},
			want:     false,
		},
		{
			name:     "persisted other content rejects empty inbound content",
			existing: domain.ConversationMessage{Text: "hi", Origin: domain.MessageOriginHuman, DeliveryContentJSON: string(encodedOther)},
			msg:      ports.ChatUserMessage{Text: "hi", Origin: domain.MessageOriginHuman},
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := legacyMessageMatches(tc.existing, tc.msg); got != tc.want {
				t.Fatalf("legacyMessageMatches = %v, want %v", got, tc.want)
			}
		})
	}
}

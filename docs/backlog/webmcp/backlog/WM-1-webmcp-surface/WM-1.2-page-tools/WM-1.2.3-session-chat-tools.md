# WM-1.2.3 · Task: Session chat tools

**Status:** ⬜ todo — [#475](https://github.com/petarnenov/ultra-factory/issues/475)

**Type:** frontend. **Estimate:** 1 pd. **Story:** WM-1.2.

## Goal

Register WebMCP tools on the session chat surface for reading conversation and
composer state, typing, accepting or dismissing the prompt suggestion,
sending, and retrying a failed turn.

## Context (verified)

- Mount chain: `LocalSessionHost` (`routes/_shell.tsx:139,158`) → `SessionView`
  (`data-testid="session-detail"`, `SessionView.tsx:1915`) →
  `SessionChatSurface` (`components/chat/SessionChatSurface.tsx:105`) →
  `ChatWorkspace` (line 650) → `ChatComposer` (`ChatWorkspace.tsx:1760`,
  `components/chat/ChatComposer.tsx:178`) → `ComposerEditor` (Lexical,
  `role="combobox"` "Message the agent").
- Composer handle: `ChatComposerHandle {focus, isEmpty, setDraft(text)}`
  (`ChatComposer.tsx:171-176`, impl 1055-1067), reached through
  `composerFocusRef` (`ChatWorkspace.tsx:1121`). Editor handle
  `ComposerEditorHandle {focus, clear, setText, insertToken, ...}`
  (`ComposerEditor.tsx:71`).
- Prompt suggestion: `useConversationPromptSuggestion` (`hooks/
  useConversationPromptSuggestion.ts:23-87`) → `{id, text, onDismiss}`,
  passed down to `ComposerEditor` as `promptSuggestion`
  (`ComposerEditor.tsx:69`), rendered as `data-testid="prompt-suggestion"`
  (line 672). Accept: `complete(event,"Tab")` (lines 499-522) inserts
  `promptSuggestion.text` when the draft is empty, then
  `onDismissSuggestion()`. Dismiss: `KEY_ESCAPE_COMMAND` (554-561) →
  `dismissSuggestion` (642-647).
- Send: `ChatComposer.submit()` (~1131) → `onSend` prop (1316-1323) →
  `SessionChatSurface.onSend` (698) → `commands.send(...)`
  (`hooks/useConversation.ts:1057`, POST `/conversation/messages`, line 599).
- Retry: `commands.retryControl = {retry(turnId), pending, error, turnId}`
  (`useConversation.ts:1117-1145`); mutation at 945 POSTs
  `/conversation/turns/{turnId}/retry`. Button `aria-label="Retry this turn"`,
  `data-testid="retry-turn"` (`ChatTimelineItems.tsx:370-382`), disabled when
  a turn runs or `retryControl.pending` (`ChatWorkspace.tsx:3844-3862`).
- Conversation state: `useConversation(sessionId, hostId)`
  (`useConversation.ts:445`) → `snapshot: ConversationSnapshot`
  (`types/conversation.ts:777`): `controller.state`, `turns[{id,state,
  errorMessage}]`, `items[{kind:"message", id, turnId, role, text, streaming}]`.

## Blueprint

```ts
// frontend/src/renderer/webmcp/tools/chat.ts
export type ChatRead = {
  sessionId: string;
  controller: string;                               // snapshot.controller.state
  composer: { text: string; focused: boolean; suggestion: string | null };
  turns: { id: string; state: string; errorMessage?: string; retryError?: string }[];
  messages: { id: string; turnId: string; role: string; text: string; streaming: boolean }[];
};

export function chatTools(ctx: {
  sessionId: string;
  snapshot: () => ConversationSnapshot | undefined; // queryClient.getQueryData(conversationQueryKey)
  composerText: () => string;                       // editor.getEditorState → text
  editor: ComposerEditorHandle;                     // setText / clear / focus
  suggestion: () => ComposerPromptSuggestion | null;
  acceptSuggestion: () => void;                     // same body as complete(...,"Tab") minus the event
  dismissSuggestion: () => void;                    // ComposerEditor dismissSuggestion
  submit: () => Promise<void>;                      // ChatComposer submit()
  retry: (turnId: string) => Promise<void>;         // commands.retryControl.retry
  retryError: () => string | undefined;             // commands.retryControl.error?.message
  subscribe: (cb: () => void) => () => void;        // conversation query cache + editor update listener
}): ToolDescriptor[] {
  return [
    { name: "chat.read", description: "Return conversation turns, messages, composer text and the prompt suggestion.", ... },
    { name: "chat.type", description: "Replace the composer draft with text.",
      inputSchema: { type: "object", required: ["text"], properties: { text: { type: "string" } } },
      execute: ({ text }) => { ctx.editor.setText(text); return { text }; } },
    { name: "chat.accept_suggestion", description: "Insert the current prompt suggestion into the composer.",
      execute: () => { if (!ctx.suggestion()) throw new ToolError("precondition", "no suggestion"); ctx.acceptSuggestion(); return { text: ctx.composerText() }; } },
    { name: "chat.dismiss_suggestion", description: "Dismiss the current prompt suggestion.", ... },
    { name: "chat.send", description: "Send the composer draft.", ... },
    { name: "chat.retry", description: "Retry a failed turn.",
      inputSchema: { ..., required: ["turnId"] }, execute: async ({ turnId }) => { await ctx.retry(turnId).catch(() => {}); return { retryError: ctx.retryError() ?? null }; } },
    { name: "chat.wait_for", description: "Resolve when a predicate on chat state holds, or fail after timeoutMs.",
      inputSchema: { ..., required: ["where"], properties: { where: { type: "string", enum: ["suggestion_visible","suggestion_hidden","composer_equals","retry_error_visible","message_sent"] }, value: { type: "string" }, timeoutMs: { type: "number" } } }, ... },
  ];
}
```

- `acceptSuggestion` is extracted from `complete()` in `ComposerEditor.tsx:499-522`
  into a function both the Tab command and the tool call; no synthetic key
  events.
- Registration: `chat.type/accept/dismiss/read(composer part)` inside
  `ComposerEditor` (has the editor and the suggestion); `chat.send`, `chat.retry`,
  `chat.read` (conversation part) in `SessionChatSurface` (has `commands`).
  `chat.read` merges both halves via a tiny shared module-level store keyed by
  `sessionId` in `tools/chat.ts` so the tool is one descriptor.
- `message_sent` is observed through `snapshot().items` growing with a user
  message whose `text` equals `value` (the e2e spec intercepts the POST; the
  catalog records the sent text through the same `page.route` as the original
  spec, see WM-2.1.1).

## Subtasks

- [ ] Extract `acceptSuggestion()` in `ComposerEditor.tsx` and reuse it in
  the Tab handler.
- [ ] `tools/chat.ts` with the seven descriptors and the merge store.
- [ ] Wire halves in `ComposerEditor` and `SessionChatSurface`; unregister on
  unmount and on `sessionId` change.
- [ ] Vitest: accept with empty draft inserts text and dismisses; accept with
  non-empty draft throws `precondition` (mirrors `complete()` guard); `retry`
  surfaces the 409 message.
- [ ] e2e `webmcp-chat.spec.ts` @P0 reusing `openChat` from
  `chat-prompt-suggestion.spec.ts` (export it to `e2e/support/open-chat.ts`):
  `callTool("chat.accept_suggestion")` → `chat.read().composer.text ===
  prompt`; `callTool("chat.send")` → `sent` equals `[prompt]`.

## Acceptance criteria

- [ ] Tools present only on a session route and only for the mounted session.
- [ ] Every chat scenario step in the catalog maps to one tool or one route
  stub step.
- [ ] `chat-prompt-suggestion.spec.ts` and `chat-retry.spec.ts` pass unchanged
  after the `acceptSuggestion` extraction.

## Verification

- `npm --prefix frontend run test:e2e -- e2e/chat-prompt-suggestion.spec.ts e2e/chat-retry.spec.ts e2e/webmcp-chat.spec.ts`
- `make ci-check`

## Dependencies

WM-1.1.1.

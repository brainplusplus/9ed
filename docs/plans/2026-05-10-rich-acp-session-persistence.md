# Rich ACP Session Persistence Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Persist full-fidelity ACP chat session state so refresh/history restore can reconstruct rich transcript, config/model controls, and then reconnect/resume live ACP session on top of restored state.

**Architecture:** Extend backend persistence from flat user/assistant messages into rich event entries plus per-session UI snapshot. Store normalized transcript entries and session snapshot in SQLite, expose them through chat history APIs, hydrate frontend session state from persisted transcript first, then resume/live-connect ACP session and merge new events onto the restored transcript without losing fidelity.

**Tech Stack:** Go, SQLite, ACP session manager, React 18, TypeScript, Zustand, Vitest

---

## Task 1: Define persistence model for rich transcript and snapshot

**Files:**
- Modify: `internal/chat/store.go`
- Modify: `internal/chat/store_test.go`
- Modify: `frontend/src/types.ts`

**Step 1: Add failing backend tests for rich transcript storage**

Add tests covering:
- saving/retrieving transcript entries with roles `user`, `assistant`, `tool_call`, `plan`
- saving tool call status/content/locations/diffs/thinking payloads
- saving/restoring session snapshot fields like `commands` and `configOptions`

**Step 2: Extend SQLite schema**

Add normalized storage for:
- `chat_events` or equivalent rich transcript table keyed by session id + timestamp/order
- `chat_session_snapshots` or additional JSON column on `chat_sessions` for commands/config options/current values

**Step 3: Add Go record types**

Define backend record structs for:
- rich transcript entry
- persisted session snapshot

**Step 4: Add CRUD methods**

Implement store methods to:
- append transcript entries
- update tool call / diff / title / snapshot state
- fetch full transcript for session
- fetch session snapshot for restore

**Step 5: Run tests and verify GREEN**

Run:

```bash
go test ./internal/chat
```

Expected: rich persistence tests pass.

---

## Task 2: Persist rich ACP event stream on backend/history API boundary

**Files:**
- Modify: `internal/httpapi/chatapi.go`
- Modify: `internal/chat/agent.go`
- Modify: `internal/httpapi/router_test.go`

**Step 1: Add failing API tests for rich event persistence**

Cover these cases:
- session title updates persist for existing session
- config options snapshot persists
- commands snapshot persists
- transcript fetch returns tool call / plan / assistant entries in order

**Step 2: Decide persistence hook**

Persist from backend event boundary, not just frontend message save path. Use chat WebSocket event flow / session event stream as canonical source for ACP richness.

**Step 3: Persist event types**

At minimum persist:
- text / assistant content evolution final form
- thinking content
- tool_call creation/update
- diffs
- plans
- title updates
- config_options snapshot
- commands snapshot

**Step 4: Keep frontend POST `/api/chat/history` for user input/context**

Use existing POST path for user messages/context, but make backend ACP event stream responsible for richer assistant-side transcript fidelity.

**Step 5: Verify tests**

Run:

```bash
go test ./internal/httpapi ./internal/chat
```

Expected: API and persistence tests pass.

---

## Task 3: Expose rich transcript + snapshot through frontend API

**Files:**
- Modify: `frontend/src/api.ts`
- Modify: `frontend/src/types.ts`

**Step 1: Add failing frontend contract tests if needed**

If no direct API tests exist, cover through store tests in next task.

**Step 2: Extend history response types**

Add frontend types for:
- rich transcript entry
- restored session snapshot
- persisted tool call / diff / plan payloads

**Step 3: Add rich session fetch API**

Provide endpoint helper returning:
- session metadata
- transcript entries
- commands snapshot
- configOptions snapshot

**Step 4: Keep old flat message API only if needed during migration**

Avoid breaking unrelated callers; deprecate flat usage in chat restore path.

---

## Task 4: Hydrate restored/history sessions from rich transcript

**Files:**
- Modify: `frontend/src/stores/chat.ts`
- Modify: `frontend/src/components/chat/ChatMessage.tsx`
- Modify: `frontend/src/hooks/useChatSession.ts`
- Test: `frontend/src/stores/chat.test.ts`

**Step 1: Add failing store tests for rich restore**

Cover:
- restore builds assistant/tool_call/plan entries from persisted transcript
- restored session title uses persisted snapshot/title
- commands and configOptions restore into active session before websocket resumes
- history-selected session matches original rich transcript structure

**Step 2: Build transcript hydration mapper**

Map persisted transcript entries into `ChatMessage[]` preserving:
- role
- thinking
- tool call metadata and content
- diffs on assistant/tool entries
- plan entries
- timestamps/order

**Step 3: Hydrate session snapshot**

Restore `commands`, `configOptions`, and title into `ChatSessionInfo` immediately on load.

**Step 4: Merge live events on top of restored transcript**

Keep existing live event handling, but ensure restored transcript does not get duplicated or overwritten incorrectly when resumed ACP events arrive.

**Step 5: Verify frontend tests**

Run:

```bash
npm test -- frontend/src/stores/chat.test.ts
```

Expected: rich restore tests pass.

---

## Task 5: Restore latest session faithfully on refresh/history selection

**Files:**
- Modify: `frontend/src/hooks/useWorkspaceStatePersistence.ts`
- Modify: `frontend/src/components/chat/ChatSessionList.tsx`
- Modify: `frontend/src/components/chat/AgentPicker.tsx`
- Test: `frontend/src/hooks/useWorkspaceStatePersistence.test.tsx`
- Test: `frontend/src/components/chat/ChatSessionList.test.tsx`

**Step 1: Add failing tests for faithful latest restore**

Cover:
- latest session restore loads persisted rich transcript before ACP reconnect
- history trigger shows connecting state during reconnect
- config/model controls visible from restored snapshot before new ACP updates arrive

**Step 2: Ensure latest-session selection uses rich restore API**

Do not reconstruct from flat assistant/user messages anymore.

**Step 3: Keep connecting/resume UX explicit**

History/session picker should show connecting state while ACP session is being resumed.

**Step 4: Verify ACP controls after restore**

Config/model controls should bind to restored `configOptions` snapshot immediately, then refresh from live ACP events when available.

---

## Task 6: Verification

**Files:**
- Verify: modified backend/frontend files above

**Step 1: Run frontend focused tests**

```bash
npm test -- frontend/src/stores/chat.test.ts frontend/src/hooks/useWorkspaceStatePersistence.test.tsx frontend/src/components/chat/ChatSessionList.test.tsx
```

**Step 2: Run backend focused tests**

```bash
go test ./internal/chat ./internal/httpapi
```

**Step 3: Run typecheck**

```bash
npm run typecheck
```

**Step 4: Run build**

```bash
npm run build
```

**Step 5: Manual acceptance checklist**

- New ACP session with tool call refreshes into identical transcript shape
- History reload shows tool calls / plans / diffs, not only flat assistant text
- Latest session auto-restores after refresh
- Restored chat footer shows correct ACP config/model controls
- Resume then continue chat appends new live events onto restored transcript cleanly

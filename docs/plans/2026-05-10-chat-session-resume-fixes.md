# Chat Session Resume Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make chat session restore/resume reliable per project, ensure history selection restores exact ACP agent/context, and fix clipped unreadable session history dropdown UI.

**Architecture:** Keep persisted chat history records as source of truth for restore/history selection, while treating live runtime session IDs as transport-only identifiers for WebSocket communication. Add explicit persisted record identity to frontend chat session state, keep restore/history APIs project-aware, and move history dropdown rendering to viewport-level overlay positioning so layout overflow cannot clip it.

**Tech Stack:** Go HTTP API, SQLite chat store, React 18, TypeScript, Zustand, Vitest, CSS

---

## Task 1: Baseline test scaffolding for chat restore behavior

**Files:**
- Create: `frontend/src/stores/chat.test.ts`
- Create: `frontend/src/hooks/useWorkspaceStatePersistence.test.tsx`
- Verify: `package.json`

**Step 1: Add chat store test harness**

Create `frontend/src/stores/chat.test.ts` with mocked `../api` functions for:
- `getChatHistory`
- `getChatSessionMessages`
- `getRestorableChatSession`
- `resumeChatSession`
- `saveChatMessage`
- `deleteChatHistory`

Reset Zustand store state between tests so tests do not leak session state.

**Step 2: Write failing test for restore preserving persisted record identity**

Add test proving `restoreSessionForProject('/repo', 'record-1')` creates active session with:
- live runtime `id = 'live-9'`
- persisted `recordId = 'record-1'`
- `agentId = 'claude'`

Expected pre-fix failure: no `recordId` field exists, or restored session identity cannot be persisted separately from runtime ID.

**Step 3: Write failing test for history selection preserving exact agent**

Add test proving `loadHistorySession('record-2')` resumes chosen history session and creates active session using:
- `recordId = 'record-2'`
- `id = 'live-22'`
- `agentId = 'opencode'`
- `acpSessionId` from history entry

Expected pre-fix failure: session identity/agent fallback incorrect.

**Step 4: Write failing test for workspace persistence saving record ID, not runtime ID**

In `useWorkspaceStatePersistence.test.tsx`, mock workspace/chat/api layers and verify saved workspace state writes `lastChatSessionId` as persisted record ID when active session has:

```ts
{ id: 'live-9', recordId: 'record-1' }
```

Expected pre-fix failure: current code saves `activeSessionId` (`live-9`).

**Step 5: Run tests and confirm RED**

Run:

```bash
npm test -- frontend/src/stores/chat.test.ts frontend/src/hooks/useWorkspaceStatePersistence.test.tsx
```

Expected: failures mentioning missing/incorrect persisted session identity handling.

---

## Task 2: Introduce explicit persisted session record identity

**Files:**
- Modify: `frontend/src/types.ts`
- Modify: `frontend/src/stores/chat.ts`
- Modify: `frontend/src/hooks/useWorkspaceStatePersistence.ts`
- Modify: `frontend/src/components/chat/ChatPanel.tsx`

**Step 1: Extend chat session type**

Add `recordId: string` to `ChatSessionInfo`. Meaning:
- `recordId` = persisted chat history session ID in SQLite
- `id` = current active/live runtime session ID used by WebSocket

**Step 2: Preserve record ID in store session creation paths**

Update store logic so:
- direct live session creation sets `recordId = id`
- history load sets `recordId = history session ID`
- project restore sets `recordId = restore.sessionId`
- existing-session lookup can match by `id` or `recordId` where appropriate

**Step 3: Persist workspace using record identity**

Change workspace persistence so `lastChatSessionId` uses active session `recordId` first, then `id` as fallback only for brand-new live sessions.

**Step 4: Keep new-chat behavior consistent**

When `ChatPanel.tsx` creates a new live chat session, include `recordId: id` in created session object.

**Step 5: Run tests and confirm GREEN for identity persistence tests**

Run:

```bash
npm test -- frontend/src/stores/chat.test.ts frontend/src/hooks/useWorkspaceStatePersistence.test.tsx
```

Expected: previously failing identity tests pass.

---

## Task 3: Fix restore and history resume data flow

**Files:**
- Modify: `frontend/src/stores/chat.ts`
- Modify: `frontend/src/api.ts`

**Step 1: Remove unsafe `unknown` agent fallback for resumable history/restore paths**

When loading history or restoring project session:
- prefer API/history agent ID
- if missing, keep prior persisted value only if actual record exists
- never silently replace known resumed session with `'unknown'` when source record available

**Step 2: Match existing sessions by record identity too**

Prevent duplicate session entries when same persisted record is reopened with new live runtime ID.

**Step 3: Keep ACP metadata attached to resumed sessions**

Ensure resumed/history-loaded sessions retain:
- `recordId`
- `acpSessionId`
- `kind`
- exact `agentId`

**Step 4: Tighten restore semantics around preferred session mismatch**

Allow backend latest-session fallback only when no preferred persisted session exists, but keep per-project scope. Keep clear error state when explicit preferred record cannot be restored.

**Step 5: Add/expand tests for duplicate-prevention and agent fidelity**

Extend `frontend/src/stores/chat.test.ts` with coverage for:
- no duplicate session rows when record resumes to new live ID
- chosen history session activates exact stored agent

**Step 6: Run focused tests**

Run:

```bash
npm test -- frontend/src/stores/chat.test.ts
```

Expected: store resume/history tests pass.

---

## Task 4: Make chat history API project-aware and latest-per-project reliable

**Files:**
- Modify: `internal/httpapi/chatapi.go`
- Modify: `internal/chat/store.go`
- Modify: `internal/chat/store_test.go`
- Modify: `frontend/src/api.ts`
- Modify: `frontend/src/stores/chat.ts`

**Step 1: Add project-scoped history listing support**

Support optional `workDir` query parameter in `GET /api/chat/history` and use `SessionsForProject(workDir, limit)` when present.

**Step 2: Cover latest-per-project store behavior**

Add Go tests proving:
- `GetLastSessionForProject(workDir)` returns most recently updated record for that project only
- `SessionsForProject(workDir, limit)` excludes other projects

**Step 3: Pass current project path when loading history for workspace chat UI**

Update frontend history-loading API/store signatures to accept optional `workDir`, then load project-scoped history during restore/history selection flows.

**Step 4: Keep existing global fallback only where needed**

Do not broaden scope beyond current UI path. If chat panel knows active project, use project-scoped history first.

**Step 5: Run RED then GREEN on Go tests**

Run:

```bash
go test ./internal/chat ./internal/httpapi
```

Expected before implementation: new project-scope tests fail.
Expected after implementation: pass.

---

## Task 5: Fix session history dropdown rendering and readability

**Files:**
- Modify: `frontend/src/components/chat/ChatSessionList.tsx`
- Modify: `frontend/src/styles/ide/chat.css`
- Modify: `frontend/src/styles/ide/polish.css`
- Create: `frontend/src/components/chat/ChatSessionList.test.tsx`

**Step 1: Write failing UI test for dropdown rendering**

Create `ChatSessionList.test.tsx` that mounts component with history entries and verifies opening session picker renders dropdown content in document body / fixed overlay path, not clipped inline-only assumptions.

If portal assertions are too brittle, assert stable class/attribute contract for overlay mode and presence of resumable badge text.

**Step 2: Move dropdown to overlay-safe rendering**

Implement fixed-position or portal-backed dropdown anchored to trigger button using viewport coordinates from `getBoundingClientRect()`.

**Step 3: Add close-on-outside / close-on-resize behavior**

Keep UX tidy and avoid stale anchor placement.

**Step 4: Improve readability styling**

Add styles for:
- resumable badge
- wider/cleaner row spacing
- safer max height
- no header wrapping that collapses dropdown anchor

Keep visual changes narrow and session-focused.

**Step 5: Remove clipping causes only if still needed after overlay move**

Prefer overlay fix first. Only loosen `overflow: hidden` if portal/fixed positioning still needs it.

**Step 6: Run UI tests**

Run:

```bash
npm test -- frontend/src/components/chat/ChatSessionList.test.tsx
```

Expected: dropdown overlay behavior and history badge tests pass.

---

## Task 6: Diagnostics, typecheck, and full verification

**Files:**
- Verify: `frontend/src/stores/chat.ts`
- Verify: `frontend/src/hooks/useWorkspaceStatePersistence.ts`
- Verify: `frontend/src/components/chat/ChatSessionList.tsx`
- Verify: `frontend/src/components/chat/ChatPanel.tsx`
- Verify: `frontend/src/types.ts`
- Verify: `frontend/src/api.ts`
- Verify: `internal/httpapi/chatapi.go`
- Verify: `internal/chat/store.go`
- Verify: tests added in Tasks 1, 4, 5

**Step 1: Run TypeScript typecheck**

```bash
npm run typecheck
```

Expected: no TypeScript errors.

**Step 2: Run focused frontend tests**

```bash
npm test -- frontend/src/stores/chat.test.ts frontend/src/hooks/useWorkspaceStatePersistence.test.tsx frontend/src/components/chat/ChatSessionList.test.tsx
```

Expected: all pass.

**Step 3: Run focused Go tests**

```bash
go test ./internal/chat ./internal/httpapi
```

Expected: all pass.

**Step 4: Run full build checks**

```bash
npm run build
```

Expected: Vite build succeeds.

**Step 5: Final requirement checklist**

Verify manually from code + tests:
- reload uses latest session per project
- workspace persistence saves persisted session record ID
- selecting history resumes exact agent/context for chosen session
- chat header/input reflect chosen session agent
- history dropdown no longer clipped and more readable

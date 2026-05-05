# Handoff Document — go-webttyd Session 2

**Date**: 2026-05-05
**Previous Session**: ACP implementation complete, all agents supported, UI improvements
**Next Session Goals**: Remaining UX features

---

## Current State

### What's Been Built (This Session)

| Feature | Status | Key Files |
|---------|--------|-----------|
| ACP Client (JSON-RPC 2.0 over stdio) | ✅ Complete | `internal/chat/acp/` |
| All 7 agents (OpenCode, Claude, Codex, Gemini, Pi, Amp, Copilot) | ✅ Complete | `internal/chat/agents.go` |
| Auto-install ACP adapters (npm) | ✅ Complete | `internal/chat/acpinstall/` |
| Dynamic configOptions (model, mode, thinking) | ✅ Complete | `internal/chat/agent.go`, `AgentPicker.tsx` |
| Slash commands (/ autocomplete) | ✅ Complete | `ChatInput.tsx` |
| Tool call cards (separate entries like Zed) | ✅ Complete | `ChatMessage.tsx`, `stores/chat.ts` |
| Plan blocks (collapsible, progress) | ✅ Complete | `ChatMessage.tsx` |
| Thinking display (collapsible) | ✅ Complete | `ChatMessage.tsx` |
| Markdown rendering (react-markdown) | ✅ Complete | `ChatMessage.tsx` |
| Text buffering (50ms batch, fix garbled) | ✅ Complete | `internal/chat/agent.go` |
| Race condition fix (promptDone channel) | ✅ Complete | `internal/chat/agent.go` |
| Git diff fix (new files return empty) | ✅ Complete | `internal/httpapi/gitapi.go` |
| File explorer git coloring | ✅ Complete | `FileTree.tsx` |
| @ mentions + file attach | ✅ Complete | `ChatInput.tsx` |
| Auto-title sessions | ✅ Complete | `stores/chat.ts` |
| Loading states (spinner, dots) | ✅ Complete | `ChatPanel.tsx`, `AgentPicker.tsx` |
| Autokill port | ✅ Complete | `cmd/server/main.go` |
| Session status icons | ✅ Complete | `ChatSessionList.tsx` |
| Gemini CLI support | ✅ Complete | `agents.go`, `agentconfig/gemini.go` |

---

## Remaining Tasks (Next Session)

### 1. Git Diff View for New Files (Untracked)
**Problem**: When clicking an untracked file in git panel, it should show the file content as-is (green, all added) instead of erroring.
**Current behavior**: Backend returns empty string for HEAD content, frontend shows empty diff.
**Expected**: Show file content on the right side (all green/added lines), left side empty.
**Files to modify**: `frontend/src/components/git/GitPanel.tsx` — the `handleFileClick` already gets empty `headContent` for new files. The Monaco DiffEditor should show this correctly (empty left, full right = all green). May need to verify the diff editor component handles this case.

### 2. Queue Message (Like Zed)
**Problem**: When agent is busy (streaming), user should be able to type and queue messages that get sent after current response completes.
**Expected UX**:
- While streaming, user can type and press Enter
- Message appears in "Queued Messages" section below chat
- When agent finishes (done event), queued messages auto-send one by one
- User can edit/delete/reorder queued messages
- "Send Now" button to force-send immediately (interrupts current)
**Files to modify**: 
- `frontend/src/stores/chat.ts` — add `queuedMessages` per session
- `frontend/src/components/chat/ChatInput.tsx` — allow send while streaming → queue
- `frontend/src/components/chat/ChatPanel.tsx` — render queue UI
- `frontend/src/hooks/useChatSession.ts` — auto-send queued on done event

### 3. .gitignore Coloring in File Explorer
**Problem**: Files/dirs that match .gitignore should be dimmed (gray/low opacity) in file tree.
**Approach**: 
- Backend: add endpoint or extend file tree API to include `ignored: boolean` per entry
- Or: read `.gitignore` patterns in frontend and match client-side
- Better: use `git check-ignore` command in backend
**Files to modify**:
- `internal/git/` — add `CheckIgnored(paths []string) []bool` method
- `internal/httpapi/fileapi.go` — extend tree response with `ignored` field
- `frontend/src/types.ts` — add `ignored?: boolean` to `DirEntry`
- `frontend/src/components/sidebar/FileTree.tsx` — dim ignored files

### 4. Config Option: Include Gitignored Files in @ Mentions
**Problem**: @ mention file picker should respect .gitignore by default, with option to include all.
**Approach**:
- Add a checkbox/toggle in the config bar or chat settings
- Store preference in session state
- Filter @ mention results based on preference
**Files to modify**:
- `frontend/src/components/chat/ChatInput.tsx` — filter mention results
- `frontend/src/stores/chat.ts` — add `includeIgnored` preference

---

## Architecture Notes

### ACP Flow
```
Browser ↔ WebSocket /ws/chat/:id ↔ Go Backend (SessionManager)
                                        ↓
                                  NewChatSession(agent, workDir)
                                        ↓
                              ┌── SupportsACP? ──┐
                              │ YES               │ NO (or fails)
                              ▼                   ▼
                         acpSession          ptySession
                              │                   │
                    ACP adapter (stdio)    raw PTY / stream-json
                              │                   │
                         ChatEvent ←── same interface ──→ ChatEvent
                              │
                    50ms text buffering
                              │
                         WebSocket → Browser
```

### Key Design Decisions
- Tool calls are **separate entries** in message list (like Zed), not nested in assistant message
- Text chunks are **buffered 50ms** before sending to prevent React state loss
- `promptDone` channel ensures `done` event arrives AFTER all notifications (no race)
- ACP adapters auto-install via npm on first use
- PTY fallback uses `--print --output-format stream-json` for Claude/Gemini

### Agent ACP Commands
| Agent | ACP Command | Native? |
|-------|-------------|---------|
| OpenCode | `opencode acp` | Yes |
| Claude | `claude-agent-acp` | npm adapter |
| Codex | `codex-acp` | npm adapter |
| Gemini | `gemini --experimental-acp` | Yes |
| Pi | `pi-acp` | npm adapter |
| Amp | `amp-acp` | npm adapter |
| Copilot | `github-copilot-cli` | npm adapter |

---

## How to Continue

```bash
# Start fresh session, reference this file:
# "Continue from docs/HANDOFF-2.md — implement remaining features"
```

# Git Panel, Chat UI & Responsive Layout — Design Spec

**Date**: 2026-05-04
**Status**: Approved
**Scope**: IDE mode (`MODE=full`) only

---

## Overview

Add three major features to the IDE mode:

1. **Git Panel** — Full Source Control UI (status, stage, commit, push, pull, branches, merge, stash, history, diff) with git gutter decorations in the editor
2. **Chat UI** — Side panel + inline code prompt, powered by CLI agent harness (OpenCode, Claude Code)
3. **Responsive Layout** — Breakpoint-based: desktop (full), tablet (collapsed overlays), mobile (single-panel navigation)

## Implementation Phases

- **Phase 1**: Git backend + Git panel UI + git gutter
- **Phase 2**: Chat backend (agent harness) + Chat UI (side panel + inline)
- **Phase 3**: Responsive breakpoints

Each phase is independently deliverable and testable.

---

## 1. Architecture

### 1.1 Backend — New Go Packages

```
internal/
├── git/              # Git operations wrapper (exec CLI)
│   ├── git.go            # Core exec helper, repo detection
│   ├── log.go            # git log, commit history parsing
│   ├── status.go         # status, stage, unstage
│   ├── branch.go         # branch CRUD, switch, merge
│   ├── diff.go           # file diff, line-level diff for gutter
│   ├── remote.go         # push, pull, fetch
│   └── stash.go          # stash list, apply, drop
├── chat/             # Agent harness
│   ├── manager.go        # Session lifecycle, spawn/kill agents
│   ├── session.go        # Single agent session, PTY-based I/O
│   ├── agents.go         # Agent registry & discovery
│   └── parser.go         # Parse agent output → structured messages
└── httpapi/
    ├── gitapi.go         # /api/git/* endpoints
    └── chatapi.go        # /ws/chat/* WebSocket handler
```

### 1.2 Frontend — New Components & Stores

```
frontend/src/
├── components/
│   ├── git/
│   │   ├── GitPanel.tsx          # Main Source Control panel (sidebar)
│   │   ├── GitStatusList.tsx     # Staged/unstaged file list
│   │   ├── GitCommitBox.tsx      # Commit message input + actions
│   │   ├── GitLogView.tsx        # Commit history timeline
│   │   ├── GitBranchPicker.tsx   # Branch switch/create/delete
│   │   ├── GitDiffView.tsx       # Inline diff viewer (Monaco diff editor)
│   │   └── GitStashPanel.tsx     # Stash list + actions
│   ├── chat/
│   │   ├── ChatPanel.tsx         # Side panel (right), conversation view
│   │   ├── ChatMessage.tsx       # Single message bubble (markdown rendered)
│   │   ├── ChatInput.tsx         # Auto-resize input + send/stop
│   │   ├── ChatSessionList.tsx   # Session picker / new chat
│   │   ├── InlinePrompt.tsx      # Floating prompt on code selection
│   │   └── AgentPicker.tsx       # Agent selector (OpenCode / Claude)
│   └── editor/
│       └── GitGutter.tsx         # Gutter decoration overlay via Monaco API
├── stores/
│   ├── workspace.ts   # Extend: activePanel type, chatVisible
│   ├── git.ts         # Git state (status, log, branches, diff cache)
│   └── chat.ts        # Chat state (sessions, messages, active agent)
├── hooks/
│   ├── useGitStatus.ts       # Polling (5s) + on-save git status refresh
│   ├── useGitGutter.ts       # Line diff for active file → decorations
│   ├── useChatSession.ts     # WebSocket chat connection
│   └── useLayoutMode.ts      # Responsive breakpoint detection
└── types.ts           # Extend with Git + Chat types
```

### 1.3 API Endpoints

#### Git Endpoints (all gated by `requireFullMode`)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/git/status` | Working tree status (staged, unstaged, untracked) |
| GET | `/api/git/log` | Commit history (paginated via `?limit=&offset=`) |
| GET | `/api/git/branches` | List branches + current branch |
| POST | `/api/git/stage` | Stage file(s) `{paths: string[]}` |
| POST | `/api/git/unstage` | Unstage file(s) `{paths: string[]}` |
| POST | `/api/git/commit` | Commit `{message: string, amend?: boolean}` |
| POST | `/api/git/push` | Push `{remote?: string, branch?: string}` |
| POST | `/api/git/pull` | Pull `{remote?: string, branch?: string}` |
| POST | `/api/git/branch` | Branch ops `{action: "create"|"delete"|"switch", name: string}` |
| POST | `/api/git/merge` | Merge `{branch: string}` |
| POST | `/api/git/stash` | Stash ops `{action: "save"|"pop"|"apply"|"drop", index?: number, message?: string}` |
| GET | `/api/git/diff` | File diff `?path=<file>` (working vs HEAD) |
| GET | `/api/git/diff-lines` | Line-level diff `?path=<file>` (for gutter decorations) |
| GET | `/api/git/blame` | Blame `?path=<file>` |
| POST | `/api/git/discard` | Discard changes `{paths: string[]}` |

#### Chat Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/chat/agents` | List detected CLI agents on system |
| POST | `/api/chat/sessions` | Create session `{agentId: string}` |
| DELETE | `/api/chat/sessions/:id` | Kill session |
| GET | `/api/chat/sessions` | List active sessions |
| WS | `/ws/chat/:sessionId` | Bidirectional chat stream |

---

## 2. Git Panel & Git Gutter

### 2.1 Git Backend Approach

Wrap `git` CLI via `os/exec` (not go-git library):
- Users already have `git` installed
- CLI output is predictable and feature-complete
- No heavy dependency needed

Security: All git operations scoped to project path within `WorkspaceRoot` (reuse `filesystem.ValidatePath` pattern).

### 2.2 Git Status Polling

```
Monaco Editor ──save event──→ useGitStatus hook
                                    │
              periodic (5s) ────────┤  fetch /api/git/status
                                    │  fetch /api/git/diff-lines
                                    ▼
                              Git Store (zustand)
                                    │
                    ┌───────────────┼───────────────┐
                    ▼                               ▼
              GitPanel                        GitGutter
              (re-render)                     (decorations)
```

- Polling interval: 5 seconds (configurable)
- On-save: immediate refresh
- Debounce: multiple saves within 1 second trigger only once

### 2.3 Git Gutter Decorations

Uses Monaco's `editor.deltaDecorations()` API.

**Decoration types**:

| Type | Visual | Color (Catppuccin) |
|------|--------|---------------------|
| Added | Solid green bar in gutter | `#a6e3a1` (green) |
| Modified | Solid blue bar in gutter | `#89b4fa` (blue) |
| Deleted | Red triangle indicator between lines | `#f38ba8` (red) |

**Data source**: `/api/git/diff-lines` returns line-level change info:

```typescript
type GutterChange = {
  startLine: number;
  endLine: number;
  type: 'added' | 'modified' | 'deleted';
};
```

### 2.4 Git Panel UI (Sidebar)

Activated via `🌿 Git` icon in Activity Bar. Panel layout:

```
┌──────────────────────────┐
│ 🌿 SOURCE CONTROL        │
├──────────────────────────┤
│ Branch: main ▾  [↑2 ↓0] │  ← current branch + ahead/behind
├──────────────────────────┤
│ ┌──────────────────────┐ │
│ │ Commit message...    │ │  ← commit input (textarea)
│ └──────────────────────┘ │
│ [✓ Commit] [▾ More]     │  ← More: amend, stash, push, pull
├──────────────────────────┤
│ STAGED CHANGES (3)       │
│   M  src/api.ts     [−] │  ← click [−] to unstage
│   A  src/new.ts     [−] │
│   D  src/old.ts     [−] │
├──────────────────────────┤
│ CHANGES (5)              │
│   M  src/App.tsx    [+] │  ← click [+] to stage
│   M  src/types.ts   [+] │
│   ?  untracked.txt  [+] │
├──────────────────────────┤
│ STASHES (1)              │
│   stash@{0}: WIP on main│
├──────────────────────────┤
│ ── HISTORY ──────────── │
│ ● abc1234 Fix bug    2h │
│ ● def5678 Add feat   1d │
│ ● ghi9012 Init       3d │
│   [Load more...]         │
└──────────────────────────┘
```

**Interactions**:
- Click file → open diff view (Monaco diff editor, split old vs new)
- `+` / `−` → stage/unstage individual file
- Context menu (right-click file) → Stage, Unstage, Discard Changes, Open File
- "Stage All" / "Unstage All" buttons in section headers
- Click commit in history → view diff snapshot for that commit
- Branch picker → dropdown with create/switch/delete
- "More" dropdown → Amend, Push, Pull, Stash Save, Stash Pop

### 2.5 Diff View

When user clicks a file in git status, open a **split diff tab** in editor area:
- Left pane: HEAD version (read-only)
- Right pane: Working version (editable)
- Inline highlighting per line (added/removed/modified)
- Uses Monaco's built-in `MonacoDiffEditor` component

---

## 3. Chat UI & Agent Harness

### 3.1 Agent Harness Concept

Backend spawns CLI agents (OpenCode, Claude Code) as **PTY subprocesses** — same pattern as existing terminal sessions, but output is parsed and streamed to chat UI.

### 3.2 Agent Discovery

Check `PATH` for known agent binaries (same pattern as `internal/shells/`):

```go
type Agent struct {
    ID      string   // "opencode", "claude"
    Label   string   // "OpenCode", "Claude Code"
    Command string   // "opencode", "claude"
    Args    []string // default args for non-interactive/pipe mode
}
```

Supported agents:
- `opencode` — OpenCode CLI
- `claude` — Claude Code CLI

### 3.3 Session Lifecycle

```
Browser                 Go Backend              CLI Agent
  │                         │                       │
  │── POST /api/chat/agents →│  (list available)     │
  │◄── [{opencode},{claude}] │                       │
  │                          │                       │
  │── POST /api/chat/sessions│                       │
  │   {agentId: "opencode"}  │── spawn PTY ────────→│
  │◄── {sessionId: "xxx"}    │                       │
  │                          │                       │
  │── WS /ws/chat/xxx ──────→│                       │
  │                          │                       │
  │── {type:"message",       │── write stdin ──────→│
  │    content:"explain X"}  │                       │
  │                          │◄── read stdout ──────│
  │◄── {type:"stream",       │   (streaming)         │
  │    content:"X is..."}    │                       │
  │◄── {type:"stream_end"}   │                       │
  │                          │                       │
  │── {type:"new_chat"} ────→│── kill old PTY ─────→│ (exit)
  │                          │── spawn new PTY ────→│ (fresh)
  │◄── {session_reset, id}   │                       │
```

**Hybrid session model**: Persistent session by default. "New Chat" kills current agent process and spawns fresh one (clean context).

### 3.4 Output Parsing

Agent CLI output is mixed markdown, ANSI codes, and progress indicators. Parser:
- Strips ANSI escape codes
- Detects message boundaries (agent-specific heuristics)
- Passes markdown content as-is to frontend for rendering
- Detects tool calls / file edits → structured `agent_action` messages

### 3.5 WebSocket Message Protocol

```typescript
// Client → Server
type ChatOutgoing =
  | { type: 'message'; content: string }
  | { type: 'message_with_context'; content: string; context: CodeContext }
  | { type: 'new_chat' }
  | { type: 'cancel' };

type CodeContext = {
  filePath: string;
  startLine: number;
  endLine: number;
  selectedCode: string;
  language: string;
};

// Server → Client
type ChatIncoming =
  | { type: 'stream'; content: string }
  | { type: 'stream_end' }
  | { type: 'agent_action'; action: string; detail: string }
  | { type: 'error'; message: string }
  | { type: 'session_reset'; newSessionId: string };
```

### 3.6 Chat Panel UI (Right Side Panel)

Toggle via `Ctrl+Shift+L` or chat icon in top-right.

```
┌──────────────────────┐
│ 💬 CHAT  [Agent ▾]   │  ← agent picker
│ [+ New] [Sessions ▾] │  ← new chat + session history
├──────────────────────┤
│                      │
│  ┌────────────────┐  │
│  │ 👤 You         │  │
│  │ Explain this   │  │
│  │ function       │  │
│  │ ┌────────────┐ │  │
│  │ │ code ctx   │ │  │  ← attached code context (collapsible)
│  │ └────────────┘ │  │
│  └────────────────┘  │
│                      │
│  ┌────────────────┐  │
│  │ 🤖 OpenCode    │  │
│  │ This function  │  │
│  │ does X by...   │  │  ← markdown rendered
│  │                │  │
│  │ ```ts          │  │
│  │ // highlighted │  │  ← syntax highlighted code blocks
│  │ ```            │  │
│  └────────────────┘  │
│                      │
│  ● Thinking...       │  ← streaming indicator
│                      │
├──────────────────────┤
│ ┌──────────────────┐ │
│ │ Ask something... │ │  ← auto-resize textarea
│ └──────────────────┘ │
│ [⏎ Send]  [■ Stop]  │
└──────────────────────┘
```

### 3.7 Inline Prompt (Code Selection)

When user selects code in Monaco editor, after 500ms delay:

```
│  const result = await fetch(url);  │
│  ██████████████████████████████████ │  ← selected
│  const data = await result.json(); │
│  ████████████████████████████████  │  ← selected
│                                    │
│  ┌───────────────────────────────┐ │
│  │ 💬 Ask about selection...     │ │  ← floating prompt
│  │ [Explain] [Refactor] [Test]   │ │  ← quick actions
│  └───────────────────────────────┘ │
```

**Behavior**:
- 500ms delay after selection stabilizes (prevents flicker during active selecting)
- Quick action buttons: Explain, Refactor, Write Tests, Fix Bug
- Custom input: type a question
- Click action → code context + prompt sent to chat panel (auto-opens if closed)
- Chat panel shows message with code context attached (collapsible block)

### 3.8 Chat State (Zustand)

```typescript
type ChatSession = {
  id: string;
  agentId: string;
  title: string;          // auto-generated from first message
  messages: ChatMessage[];
  status: 'idle' | 'streaming' | 'error';
  createdAt: number;
};

type ChatMessage = {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  context?: CodeContext;
  timestamp: number;
};

type ChatStore = {
  sessions: ChatSession[];
  activeSessionId: string | null;
  activeAgentId: string | null;
  agents: Agent[];
  chatVisible: boolean;
  // actions...
};
```

---

## 4. Responsive Layout

### 4.1 Breakpoints

| Breakpoint | Range | Label | Layout Strategy |
|------------|-------|-------|-----------------|
| Desktop | ≥1024px | Full | All panels visible, resizable |
| Tablet | 768–1023px | Compact | Sidebar & chat as slide-in overlays |
| Mobile | <768px | Single | One panel at a time, bottom nav |

### 4.2 Desktop (≥1024px)

```
┌──┬──────────┬────────────────────┬──────────┐
│A │ Sidebar  │  Editor + Gutter   │  Chat    │
│c │ (resize) │                    │  Panel   │
│t │          ├────────────────────┤ (resize) │
│  │          │  Terminal          │          │
└──┴──────────┴────────────────────┴──────────┘
```

- All panels visible by default
- Chat panel: toggle via icon or `Ctrl+Shift+L`
- Sidebar + Chat resizable via drag handles
- Minimum widths: sidebar 200px, editor 300px, chat 280px

### 4.3 Tablet (768–1023px)

```
┌──┬────────────────────────────────┐
│A │        Editor + Gutter         │
│c │                                │
│t ├────────────────────────────────┤
│  │        Terminal                │
└──┴────────────────────────────────┘
     ◄── Sidebar overlay        Chat overlay ──►
```

- Activity bar remains visible (48px, icon-only)
- Sidebar: hidden by default, click activity bar icon → slide-in overlay from left
- Chat panel: hidden by default, click chat icon → slide-in from right
- Semi-transparent backdrop, tap to dismiss
- Editor + Terminal get full width

### 4.4 Mobile (<768px)

```
┌────────────────────────┐
│  [Header / Breadcrumb] │
├────────────────────────┤
│                        │
│   Active Panel         │
│   (full screen)        │
│                        │
├────────────────────────┤
│ 📁  🌿  📝  🖥  💬  │  ← bottom nav
└────────────────────────┘
```

Bottom nav icons: Explorer | Git | Editor | Terminal | Chat

- One panel visible at a time, full-width full-height
- Bottom nav to switch panels
- Swipe left/right between panels (optional)
- Long-press on code → select + show inline prompt (no hover on mobile)
- Pull-to-refresh → refresh git status (git panel)

### 4.5 Implementation Strategy

**CSS**: Custom properties + media queries + layout mode class on root element.

```css
:root {
  --bp-tablet: 768px;
  --bp-desktop: 1024px;
}

.layout-desktop { /* full panels */ }
.layout-tablet  { /* overlays */ }
.layout-mobile  { /* single panel + bottom nav */ }
```

**React**: Custom hook `useLayoutMode()` returns `'desktop' | 'tablet' | 'mobile'`. Components conditionally render different trees per mode (not CSS hide/show — avoids mounting heavy components on mobile).

### 4.6 Touch Interactions

- Swipe from left edge → open sidebar overlay (tablet)
- Swipe from right edge → open chat overlay (tablet)
- Long-press on code → select + inline prompt (mobile)
- Pinch-to-zoom → disabled in editor (Monaco handles own zoom)
- Pull-to-refresh → refresh git status (git panel)

### 4.7 Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+B` | Toggle sidebar (existing) |
| `` Ctrl+` `` | Toggle terminal (existing) |
| `Ctrl+Shift+L` | Toggle chat panel (NEW) |
| `Ctrl+Shift+G` | Open git panel (NEW) |
| `Ctrl+Shift+I` | Inline prompt on selection (NEW) |
| `Escape` | Dismiss overlay / inline prompt |

---

## 5. Type Extensions

### 5.1 ActivePanel

```typescript
// Before
type ActivePanel = 'explorer' | 'search' | 'projects' | 'terminal';

// After
type ActivePanel = 'explorer' | 'search' | 'projects' | 'terminal' | 'git';
```

Note: Chat is NOT in ActivePanel — it's a separate right-side panel with its own `chatVisible` toggle, independent of sidebar panel selection.

### 5.2 New Types

```typescript
// Git types
type GitFileStatus = {
  path: string;
  status: 'modified' | 'added' | 'deleted' | 'renamed' | 'untracked';
  staged: boolean;
};

type GitBranch = {
  name: string;
  current: boolean;
  remote?: string;
  ahead: number;
  behind: number;
};

type GitCommit = {
  hash: string;
  shortHash: string;
  message: string;
  author: string;
  date: string;       // ISO 8601
  relativeDate: string; // "2 hours ago"
};

type GitStash = {
  index: number;
  message: string;
};

type GutterChange = {
  startLine: number;
  endLine: number;
  type: 'added' | 'modified' | 'deleted';
};

// Chat types
type Agent = {
  id: string;
  label: string;
  available: boolean;
};

type CodeContext = {
  filePath: string;
  startLine: number;
  endLine: number;
  selectedCode: string;
  language: string;
};

type ChatMessage = {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  context?: CodeContext;
  timestamp: number;
};

type ChatSession = {
  id: string;
  agentId: string;
  title: string;
  messages: ChatMessage[];
  status: 'idle' | 'streaming' | 'error';
  createdAt: number;
};
```

---

## 6. Non-Goals

- Git merge conflict resolution UI (use terminal for complex merges)
- Chat message persistence across server restarts (sessions are ephemeral)
- Multiple simultaneous chat sessions visible (one active at a time)
- Git submodule management
- Chat file editing capabilities (agent may edit files, but UI won't provide direct file edit controls in chat)

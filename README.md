# Web IDE Terminal

Browser-based IDE with terminal, git source control, AI chat, and responsive layout.

## Features

### Terminal
- PTY-backed terminal sessions managed by a Go backend
- Multi-tab terminal UI using xterm.js
- Cross-platform shell detection (PowerShell, cmd, Git Bash, WSL, bash, zsh)

### IDE Mode (Full)
- Monaco Editor with multi-tab file editing
- File explorer with tree navigation
- Full-text search across project files
- Multi-project workspace support
- Real-time file watcher (auto-sync on external changes)

### Git Source Control
- Full git panel: status, stage/unstage, commit, push, pull
- Branch management: create, switch, delete, merge
- Stash support: save, pop, apply, drop
- Commit history with pagination
- Diff view (Monaco DiffEditor, side-by-side)
- Git gutter decorations (green=added, blue=modified, red=deleted)
- File-level discard changes

### AI Chat
- Side panel with conversation UI
- Agent harness: spawns CLI agents (OpenCode, Claude Code, Codex) as PTY subprocesses
- Inline code prompt: select code → quick actions (Explain, Refactor, Test, Fix)
- Streaming responses via WebSocket
- Multiple chat sessions with "New Chat" support

### Responsive Layout
- **Desktop** (≥1024px): Full panel layout with resizable sidebar, editor, terminal, chat
- **Tablet** (768–1023px): Overlay sidebar and chat panels
- **Mobile** (<768px): Single-panel view with bottom navigation

### Security
- Basic Auth protection for UI, API, and WebSocket
- Session cookie bridge for WebSocket authentication
- Path traversal protection for filesystem operations

## Requirements

- Go 1.24+
- Node.js 20+
- Git (for source control features)
- Optional: OpenCode, Claude Code, or Codex CLI (for AI chat)

## Setup

1. Copy `.env.example` to `.env`
2. Configure environment variables
3. Install frontend dependencies: `npm install`
4. Build frontend: `npm run build`
5. Start server: `go run ./cmd/server`
6. Open `http://localhost:8080`

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP listen port | `8080` |
| `BASIC_AUTH_USERNAME` | Required username | — |
| `BASIC_AUTH_PASSWORD` | Required password | — |
| `MODE` | `simple` (terminal only) or `full` (IDE) | `simple` |
| `WORKSPACE_ROOT` | Default workspace directory | — |

## Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+B` | Toggle sidebar |
| `` Ctrl+` `` | Toggle terminal |
| `Ctrl+Shift+G` | Open git panel |
| `Ctrl+Shift+L` | Toggle chat panel |
| `F1` | Show keyboard shortcuts |
| `Ctrl+S` | Save file |
| `Ctrl+Shift+I` | Inline AI prompt (select code first) |

## Project Structure

```
cmd/server/          — Application entry point
internal/
  config/            — .env loading and validation
  auth/              — Basic Auth middleware + session cookies
  shells/            — OS-aware shell discovery
  terminal/          — PTY session spawning and management
  filesystem/        — File operations with path security
  watcher/           — Real-time file watcher (fsnotify)
  git/               — Git CLI wrapper (status, log, branch, diff, stash, blame)
  chat/              — AI agent harness (PTY subprocess management)
  httpapi/           — REST API + WebSocket handlers
  server/            — HTTP assembly and static serving
frontend/src/
  apps/ide/          — IDE mode entry (workspace, project picker)
  apps/terminal/     — Simple terminal mode
  components/
    editor/          — Monaco editor, diff view, tabs
    git/             — Git panel, status list, branch picker, stash
    chat/            — Chat panel, messages, input, inline prompt
    sidebar/         — Activity bar, file tree, search, projects
    terminal/        — Terminal panel (xterm.js)
    shared/          — Bottom nav, shortcuts help
  stores/            — Zustand state (workspace, git, chat)
  hooks/             — Custom hooks (git status, gutter, chat, layout)
```

## Development

```bash
# Run all tests
go test ./...

# Build Go binary
go build ./...

# Frontend typecheck
npm run typecheck

# Frontend build
npm run build

# Dev server (frontend hot reload)
npm run dev
```

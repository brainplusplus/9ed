# AGENTS.md — 9ed

Browser-based IDE with PTY terminal, git source control, AI chat (ACP), and Monaco editor.

## Quick Commands

```bash
npm run start          # full start (install + build frontend + run server)
npm run dev            # vite dev server (frontend hot reload, proxies to Go backend)
npm run server         # go run ./cmd/server (backend only)
npm run check          # tsc --noEmit + go vet ./...
npm run go:test        # go test ./...
npm run test           # vitest run --environment jsdom (frontend tests)
npm run build          # vite build (frontend only)
npm run server:build   # go build -o server ./cmd/server
```

### Dev workflow

Run `npm run server` and `npm run dev` in parallel. Vite proxies API/WS requests to the Go backend.

### Run a single Go test

```bash
go test ./internal/chat/ -run TestCreateAndListSessions
```

## Architecture

- **Backend**: Go, standard library `net/http` + `gorilla/websocket`. No framework.
- **Frontend**: React 18 + Vite + Monaco Editor + xterm.js + Zustand (state).
- **Module name**: `github.com/brainplusplus/9ed` (import as `github.com/brainplusplus/9ed/internal/...`).
- **Entry point**: `cmd/server/main.go` — loads `.env`, autokills existing port process, starts HTTP server.
- **Two modes**: `MODE=simple` (terminal only) vs `MODE=full` (IDE with file explorer, git, chat). Controlled by env var. "full" features are gated behind `cfg.Mode == "full"` checks in `internal/server/server.go`.

### Backend layout (`internal/`)

| Package | Purpose |
|---------|---------|
| `config/` | `.env` loading, validation. `BASIC_AUTH_USERNAME` and `BASIC_AUTH_PASSWORD` are **required** — server won't start without them. |
| `auth/` | Basic auth middleware + session cookie bridge for WebSocket auth. |
| `shells/` | OS-aware shell discovery (PowerShell, cmd, Git Bash, WSL, bash, zsh). |
| `terminal/` | PTY session spawning via `aymanbagabas/go-pty`. Session lifecycle. |
| `filesystem/` | File CRUD with path traversal protection. Recursive copy. Full-text search. |
| `watcher/` | `fsnotify`-based real-time file watcher. |
| `git/` | Git CLI wrapper (status, log, branch, diff, stash, blame, check-ignore). Executes `git` binary. |
| `chat/acp/` | ACP (Agent Client Protocol) — JSON-RPC 2.0 over stdio. `protocol.go` = types, `client.go` = transport, `adapter.go` = subprocess lifecycle. |
| `chat/acpinstall/` | Auto-installs ACP adapters via npm/pip. |
| `chat/agentconfig/` | Detects agent config files for models/providers per agent type. |
| `chat/` | Unified `ChatSession` interface (ACP + PTY fallback). `store.go` = SQLite persistence (chat history, workspace state). `agent.go` = active terminal routing, terminal command execution. |
| `httpapi/` | REST API + WebSocket handlers. All routes registered in `router.go`. |
| `server/` | HTTP assembly, static file serving (SPA handler), wires all dependencies. |
| `tunnel/` | Tunnel subprocess lifecycle — supports `bore` (fixed port via bore.pub) and `cloudflare` (quick tunnel). Auto-installs binaries. |

### Frontend layout (`frontend/src/`)

| Path | Purpose |
|------|---------|
| `apps/ide/` | IDE mode entry (workspace, project picker) |
| `apps/terminal/` | Simple terminal mode |
| `components/editor/` | Monaco editor, diff view, tab management |
| `components/git/` | Git panel, status, branches, stash, diff |
| `components/chat/` | Chat UI, agent picker, permission dialog, message queue |
| `components/sidebar/` | File tree, search, activity bar |
| `components/terminal/` | xterm.js terminal panel, tabs per project with scrollback replay |
| `components/browser/` | Browser panel, inspect overlay (4-layer box model), element selection |
| `stores/` | Zustand stores (workspace, git, chat) |
| `hooks/` | Custom hooks |
| `config/` | Monaco language setup (TS/JS diagnostics, Vue/Svelte) |

Vite builds two entry points: `frontend/index.html` (terminal) and `frontend/ide.html` (IDE). Output goes to `dist/`.

## Key Facts for Agents

- **Auth is mandatory**. Server refuses to start without `BASIC_AUTH_USERNAME` and `BASIC_AUTH_PASSWORD`. Copy `.env.example` to `.env` and fill in values.
- **SQLite storage** at `~/.9ed/ide.db` (created automatically). Uses `modernc.org/sqlite` (pure Go, no CGo required). Single connection (`MaxOpenConns=1`). WAL mode. Schema migrations in `store.go` `migrateSchema()`.
- **ACP protocol** is this project's custom protocol, not a standard. Full type definitions in `internal/chat/acp/protocol.go`. It's JSON-RPC 2.0 over stdio to agent subprocesses.
- **Git tests** (`internal/git/*_test.go`) create real temp git repos via `exec.Command("git", "init", ...)`. They require `git` on PATH.
- **No Go linting tool** configured. `go vet ./...` is the static analysis step (used in `npm run check`).
- **TypeScript strict mode** is enabled (`strict: true`, `noUnusedLocals`, `noUnusedParameters`).
- **Autokill behavior**: `AUTOKILL_PORT=true` (default) kills any existing process on the configured port before starting. Uses `netstat` on Windows, `lsof`/`fuser` on Unix.
- **Cross-platform**: Code handles Windows vs Unix differences (shell discovery, port kill, home directory).

## Environment Variables

| Variable | Required | Default | Notes |
|----------|----------|---------|-------|
| `BASIC_AUTH_USERNAME` | **Yes** | — | Server won't start without it |
| `BASIC_AUTH_PASSWORD` | **Yes** | — | Server won't start without it |
| `PORT` | No | `8080` | |
| `MODE` | No | `simple` | `simple` or `full` |
| `WORKSPACE_ROOT` | No | cwd | Default workspace directory |
| `AUTOKILL_PORT` | No | `true` | Kill existing process on port before start |
| `TUNNEL` | No | `true` | Auto-start tunnel for public access. Requires `cloudflared` or `bore` binary |
| `TUNNEL_ENGINE` | No | `bore` | Tunnel engine: `bore` (fixed port) or `cloudflare` (random URL) |
| `USE_BROWSER` | No | `false` | Enable in-app browser panel (requires Chrome/Edge/Chromium) |
| `TERMINAL_AI_MAX_LINES` | No | `100` | Max terminal lines sent to AI as context |
| `DEBUG` | No | `false` | Enable verbose debug logging (watcher events, WebSocket, cloudflared output) |
| `DEBUG_WATCHER` | No | `false` | Enable watcher-specific debug logs (requires DEBUG=true) |

## Conventions

- Go code follows standard Go style. No custom linter config.
- Go tests use `testing.T` with table-driven patterns in some packages. Test files live alongside source (`*_test.go`).
- Frontend tests use vitest with jsdom. Test files co-located as `*.test.ts(x)`.
- REST API routes follow `/api/{resource}` pattern. WebSocket routes follow `/ws/{resource}`.
- API responses are JSON. Errors return plain text via `http.Error()`.
- WebSocket messages are JSON with `{type, data}` shape (terminal) or ACP protocol (chat).

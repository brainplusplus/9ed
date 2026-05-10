# Handoff Document — 9ed Next Phase

**Date**: 2026-05-05
**Previous Session**: Implemented git panel, chat UI, responsive layout, SQLite persistence, bug fixes
**Next Session Goals**: ACP (Agent Client Protocol) implementation + LSP/linter extension system design

---

## Current State

### What's Been Built

| Feature | Status | Key Files |
|---------|--------|-----------|
| Git Source Control | ✅ Complete | `internal/git/`, `frontend/src/components/git/`, `internal/httpapi/gitapi.go` |
| Chat UI + Agent Harness | ✅ Complete | `internal/chat/`, `frontend/src/components/chat/`, `internal/httpapi/chatapi.go` |
| Responsive Layout | ✅ Complete | `frontend/src/hooks/useLayoutMode.ts`, `frontend/src/components/shared/BottomNav.tsx` |
| SQLite Persistence | ✅ Complete | `internal/chat/store.go` (ide.db: chat_sessions, chat_messages, recent_projects) |
| Shortcuts Help | ✅ Complete | `frontend/src/components/shared/ShortcutsHelp.tsx` |
| Agent Discovery | ✅ Partial | `internal/chat/agents.go` — PATH-based, resolves full binary path |

### Current Chat Architecture (PTY-based, to be replaced/augmented)

```
Browser ↔ WebSocket (/ws/chat/:id) ↔ Go Backend ↔ PTY subprocess ↔ CLI Agent
```

- `internal/chat/session.go` — spawns agent as PTY, reads stdout, writes stdin
- `internal/chat/manager.go` — session lifecycle (create/get/remove/list)
- `internal/chat/parser.go` — strips ANSI codes from output
- `internal/chat/agents.go` — discovers agents via PATH (opencode, claude, codex)
- `internal/httpapi/chatapi.go` — REST + WebSocket endpoints

### Known Issues
- PTY output is messy (ANSI codes, progress bars, prompts mixed in)
- No model picker (agent uses whatever model is configured in its own config)
- No structured communication (just raw text in/out)

---

## Next Phase 1: ACP Implementation

### What is ACP?
**Agent Client Protocol** — JSON-RPC 2.0 over stdio. Standard protocol for editor ↔ agent communication. Used by Zed, and supported by Claude Code, Codex, Pi, Gemini, OpenCode.

Reference: https://zed.dev/blog/claude-code-via-acp

### Architecture (ACP preferred + PTY fallback)

```
Browser ↔ WebSocket ↔ Go Backend (ACP Client) ↔ stdio/JSON-RPC ↔ ACP Adapter ↔ CLI Agent
                                    ↓ (fallback)
                              PTY subprocess ↔ CLI Agent (raw)
```

### Target File Structure

```
internal/chat/
├── acp/
│   ├── client.go        # JSON-RPC 2.0 over stdio client
│   ├── protocol.go      # ACP message types (requests, responses, notifications)
│   └── adapter.go       # Spawn & manage ACP adapter process
├── pty/
│   └── session.go       # Existing PTY logic (moved here, becomes fallback)
├── agent.go             # Unified Agent interface + dispatcher (ACP vs PTY)
├── manager.go           # Session manager (existing, adapt to new interface)
├── agents.go            # Agent discovery (existing)
├── parser.go            # ANSI parser (existing, for PTY fallback)
└── store.go             # SQLite persistence (existing)
```

### Unified Agent Interface

```go
type Agent interface {
    ListModels(ctx context.Context) ([]Model, error)
    Chat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
    GetInfo() AgentInfo  // name, active model, capabilities
    Close() error
}

// Dispatcher: try ACP first, fallback to PTY
func NewAgent(cfg AgentConfig) (Agent, error) {
    if cfg.ACPCommand != "" {
        if a, err := acp.New(cfg.ACPCommand, cfg.Args); err == nil {
            return a, nil
        }
    }
    return pty.New(cfg) // fallback
}
```

### ACP Adapter Commands (known)

| Agent | ACP Adapter Command | Notes |
|-------|-------------------|-------|
| Claude Code | `claude --acp` or Claude Agent SDK adapter | Built-in to Zed |
| Codex | `codex --acp` or dedicated adapter | Built-in to Zed |
| OpenCode | `opencode acp` | See https://opencode.ai/docs/acp/ |
| Pi | Dedicated ACP adapter (npm package) | See https://zed.dev/acp/agent/pi |
| Gemini | `gemini --acp` or adapter | Built-in to Zed |
| Copilot | Via gh CLI or dedicated adapter | TBD |

### JSON-RPC 2.0 Protocol Basics

```json
// Request (client → agent)
{"jsonrpc": "2.0", "id": 1, "method": "agent/listModels", "params": {}}

// Response (agent → client)
{"jsonrpc": "2.0", "id": 1, "result": {"models": [...]}}

// Notification (agent → client, no id)
{"jsonrpc": "2.0", "method": "agent/message", "params": {"content": "..."}}
```

### Key ACP Methods to Implement

| Method | Direction | Purpose |
|--------|-----------|---------|
| `initialize` | client → agent | Handshake, exchange capabilities |
| `agent/listModels` | client → agent | Get available models |
| `agent/chat` | client → agent | Send message, get streaming response |
| `agent/cancel` | client → agent | Cancel current operation |
| `agent/message` | agent → client | Streaming response chunk |
| `agent/toolUse` | agent → client | Agent wants to use a tool |
| `agent/toolResult` | client → agent | Tool execution result |

### Implementation Steps

1. Research ACP spec in detail (check Zed's open-source ACP implementation)
2. Implement JSON-RPC 2.0 client (stdio transport)
3. Implement ACP protocol types
4. Implement adapter process management (spawn, health check, restart)
5. Create unified Agent interface
6. Move existing PTY logic to `internal/chat/pty/`
7. Update manager to use new interface
8. Update WebSocket handler to bridge ACP ↔ browser
9. Update frontend to use model list from ACP
10. Test with real agents (opencode acp, claude --acp)

---

## Next Phase 2: LSP/Linter Extension System

### Problem
Monaco editor in browser cannot directly connect to LSP servers (they run as local processes). Need a WebSocket proxy.

### Proposed Architecture

```
Monaco (browser) ↔ WebSocket ↔ Go Backend (LSP Proxy) ↔ stdio ↔ LSP Server (local process)
```

### Extension System Design Questions (to brainstorm)

1. **Extension format**: How are language extensions defined? (JSON manifest? TOML config?)
2. **Discovery**: Auto-detect installed LSP servers? Or user configures manually?
3. **Bundled vs user-installed**: Ship common ones (typescript-language-server, gopls) or require user to install?
4. **Extension registry**: Like VS Code marketplace? Or just local config?
5. **Hot-reload**: Can extensions be added without restart?
6. **Per-language config**: How to configure linter rules per project?

### Known LSP Servers to Support

| Language | LSP Server | Install |
|----------|-----------|---------|
| TypeScript/JS | typescript-language-server | npm |
| Go | gopls | go install |
| Python | pyright / pylsp | pip/npm |
| PHP | intelephense / phpactor | npm/composer |
| Java | jdtls | manual |
| Ruby | solargraph | gem |
| C# | omnisharp | dotnet |
| HTML/CSS/SCSS | vscode-langservers-extracted | npm |
| Rust | rust-analyzer | rustup |

### Monaco LSP Integration

Use `monaco-languageclient` npm package which bridges Monaco ↔ LSP protocol over WebSocket. This is the standard approach.

### Target Structure

```
internal/
├── lsp/
│   ├── proxy.go         # WebSocket ↔ stdio bridge
│   ├── manager.go       # LSP server lifecycle
│   ├── registry.go      # Extension/language registry
│   └── discovery.go     # Auto-detect installed servers
frontend/src/
├── extensions/
│   ├── lsp-client.ts    # Monaco LSP client setup
│   └── registry.ts      # Extension manifest loading
```

---

## Environment Info

- **OS**: Windows (dev), but code must be multi-OS
- **Go**: 1.24+
- **Node**: 20+
- **Agents installed**: opencode (`C:\Users\brainplusplus\.bun\bin\opencode.exe`), claude (`C:\Users\brainplusplus\.local\bin\claude.exe`), codex (`C:\Program Files\nodejs\codex.ps1`)
- **Agent configs**:
  - OpenCode: `C:\Users\brainplusplus\.config\opencode\opencode.json`
  - Claude: `C:\Users\brainplusplus\.claude\settings.json`
  - Codex: `C:\Users\brainplusplus\.codex\config.toml`
- **SQLite DB**: `~/.9ed/ide.db`

---

## Key Decisions Made

1. ~~Config file reader approach~~ → **CANCELLED** — replaced by ACP
2. ACP preferred + PTY fallback (Approach B)
3. SQLite for persistence (ide.db — chat + recent projects)
4. Responsive: breakpoint-based (desktop/tablet/mobile)
5. Git: CLI wrapper via os/exec (not go-git library)
6. Chat: WebSocket streaming to browser
7. LSP: needs design brainstorm before implementation

---

## How to Continue

```bash
# Start fresh session, reference this file:
# "Continue from docs/HANDOFF.md — implement ACP client (Phase 1)"

# Or for LSP:
# "Continue from docs/HANDOFF.md — design LSP extension system (Phase 2)"
```

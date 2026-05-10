# IDE Mode Design — 9ed

**Date**: 2026-04-22
**Status**: Approved
**Approach**: Monolithic extension of existing codebase

---

## Overview

Extend 9ed from a browser-based terminal into a full web IDE with file management, Monaco code editor, and multi-project support. A `MODE` environment variable controls whether the app runs as a lightweight terminal (`simple`) or a full IDE (`full`).

## Requirements

| Requirement | Detail |
|---|---|
| Mode switching | `MODE=simple` (terminal only) / `MODE=full` (IDE) via `.env` |
| Code editor | Monaco Editor with syntax highlighting, multi-tab |
| Layout | VS Code-style 3-panel: activity bar + sidebar + editor + terminal |
| File access | Full filesystem, configurable root via `WORKSPACE_ROOT` |
| File operations | Create, rename, delete, upload, download, copy/move, search in files |
| Project concept | Project picker landing page, multi-project switching via activity bar |
| Terminal integration | Auto-cd to project folder, reuse existing multi-tab terminal |

---

## 1. Mode System

### Environment Variables

```env
MODE=simple              # "simple" (default) | "full"
WORKSPACE_ROOT=          # optional root path; empty = full filesystem
```

### How Modes Differ

| Layer | `MODE=simple` | `MODE=full` |
|---|---|---|
| Backend | Terminal APIs only | + File APIs, Search API, Config API |
| Frontend | Terminal-only UI (~200KB) | IDE UI with Monaco (~4MB) |
| Entry point | `frontend/index.html` | `frontend/ide.html` |

### Implementation

- **Two Vite entry points**: `index.html` (terminal) and `ide.html` (IDE). Separate bundles — simple mode never loads Monaco.
- **Backend conditional routing**: Go server checks `MODE` and serves the appropriate `index.html` or `ide.html` at `/`. File/search API routes return 404 in simple mode.
- **Config struct** gains `Mode` and `WorkspaceRoot` fields.

---

## 2. Project Picker

When `MODE=full`, the user lands on a project picker before entering the IDE.

### Flow

```
User opens / → Basic Auth → Project Picker → Select folder → IDE Workspace
```

### UI

- Folder tree browser starting from `WORKSPACE_ROOT` (or `/`)
- Lazy-loaded: one level at a time on expand
- "Open" button on any folder to enter IDE mode
- Recent projects list (localStorage)
- Bookmarks (localStorage)

### Terminal Auto-CD

Session creation API gains an optional `cwd` parameter:

```
POST /api/sessions  { "shellId": "pwsh", "cwd": "/home/user/myproject" }
```

---

## 3. IDE Layout

```
+----+---------+----------------------------+
|    |         | Editor Tabs: [file.go] x   |
| A  | Sidebar |----------------------------|
| c  |         |                            |
| t  | (context|      Monaco Editor         |
| i  |  changes|      (active file)         |
| v  |  based  |                            |
| i  |  on     |----------------------------|
| t  |  active | Terminal: [bash] [pwsh] x  |
| y  |  icon)  |----------------------------|
|    |         |      Terminal (xterm.js)    |
| B  |         |                            |
| a  |         |                            |
| r  |         |                            |
+----+---------+----------------------------+
```

### Activity Bar (~48px, vertical icons)

| Icon | Panel |
|---|---|
| Explorer | File tree of current project |
| Search | Search in files |
| Projects | List of opened projects — click to switch |
| Terminal | Focus/toggle terminal panel |

### Multi-Project Behavior

- Multiple projects can be open simultaneously
- Each project has its own state: open files, terminal sessions
- Click project in activity bar switches entire context
- Project state persisted in `localStorage`

### Resizing

- Vertical splitter between sidebar and main area (drag handle)
- Horizontal splitter between editor and terminal (drag handle)
- Sidebar and terminal panel are collapsible

### Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| `Ctrl+S` | Save active file |
| `Ctrl+P` | Quick file open |
| `` Ctrl+` `` | Toggle terminal |
| `Ctrl+B` | Toggle sidebar |

---

## 4. Backend File System API

All routes below are only active when `MODE=full`. They return 404 in simple mode.

### File Tree & Navigation

```
GET /api/files/tree?path=/some/dir&depth=1
    Response: [{ name, type: "file"|"dir", size, modified }]
```

### File Read/Write

```
GET /api/files/content?path=/some/file.go
    Response: { content, encoding: "utf-8", size }

PUT /api/files/content?path=/some/file.go
    Body: { content: "..." }
    Response: 200 OK
```

### File Operations (CRUD)

```
POST   /api/files/create   { path, type: "file"|"dir" }
POST   /api/files/rename   { oldPath, newPath }
POST   /api/files/copy     { sourcePath, destPath }
POST   /api/files/move     { sourcePath, destPath }
DELETE /api/files?path=/some/file.go
```

### Upload/Download

```
POST /api/files/upload?path=/target/dir
     Body: multipart/form-data
     Response: 201 Created

GET  /api/files/download?path=/some/file.go
     Response: binary (Content-Disposition: attachment)
```

### Search

```
GET /api/files/search?root=/project&query=TODO&regex=false&maxResults=100
    Response: [{ path, line, column, preview }]
```

### Config

```
GET /api/config
    Response: { mode: "full"|"simple", workspaceRoot: "/home/user" }
```

### Security

- **Path traversal protection**: all paths resolved and validated against `WORKSPACE_ROOT`
- **Symlink policy**: configurable; default is don't follow symlinks outside root
- **Max file size**: 10MB for read/write (configurable)
- **Binary detection**: return metadata only for binary files, no content
- **`.gitignore`-aware**: tree listing respects `.gitignore` by default

---

## 5. Frontend Component Architecture

### New Dependencies

```
monaco-editor            # Code editor
@monaco-editor/react     # React wrapper
zustand                  # State management (~1KB)
react-resizable-panels   # Split pane layout
```

### Component Tree

```
<App>
  +-- MODE=simple --> <TerminalApp/>          (existing, unchanged)
  +-- MODE=full  --> <IDEApp/>
                       +-- <ProjectPicker/>   (landing page)
                       +-- <IDEWorkspace/>
                             +-- <ActivityBar/>
                             |     +-- ExplorerIcon
                             |     +-- SearchIcon
                             |     +-- ProjectsIcon
                             |     +-- TerminalIcon
                             +-- <Sidebar/>
                             |     +-- <FileTree/>
                             |     +-- <SearchPanel/>
                             |     +-- <ProjectList/>
                             +-- <EditorArea/>
                             |     +-- <EditorTabs/>
                             |     +-- <MonacoEditor/>
                             +-- <TerminalPanel/>
                                   +-- <TerminalTabs/>  (reuse)
                                   +-- <TerminalView/>  (reuse)
```

### File Structure

```
frontend/src/
+-- apps/
|   +-- terminal/             # MODE=simple entry
|   |   +-- TerminalApp.tsx
|   +-- ide/                  # MODE=full entry
|       +-- IDEApp.tsx
|       +-- ProjectPicker.tsx
|       +-- IDEWorkspace.tsx
+-- components/
|   +-- terminal/             # existing, moved here
|   |   +-- TerminalTabs.tsx
|   |   +-- TerminalView.tsx
|   |   +-- TopBar.tsx
|   +-- editor/
|   |   +-- EditorArea.tsx
|   |   +-- EditorTabs.tsx
|   |   +-- MonacoEditor.tsx
|   +-- sidebar/
|   |   +-- ActivityBar.tsx
|   |   +-- Sidebar.tsx
|   |   +-- FileTree.tsx
|   |   +-- FileTreeNode.tsx
|   |   +-- SearchPanel.tsx
|   |   +-- ProjectList.tsx
|   +-- shared/
|       +-- SplitPane.tsx
+-- stores/
|   +-- workspace.ts          # Zustand store
+-- api.ts                    # extended with file APIs
+-- types.ts                  # extended with file/project types
+-- styles/
    +-- terminal.css
    +-- ide.css
    +-- shared.css
```

### Vite Multi-Entry Config

```ts
build: {
  rollupOptions: {
    input: {
      terminal: resolve(__dirname, 'frontend/index.html'),
      ide: resolve(__dirname, 'frontend/ide.html'),
    }
  }
}
```

### State Management (Zustand)

```ts
type WorkspaceState = {
  projects: Project[]
  activeProjectId: string
  activePanel: 'explorer' | 'search' | 'projects' | 'terminal'
}

type Project = {
  id: string
  path: string
  name: string
  openFiles: FileTab[]
  activeFileId: string | null
  terminalSessions: string[]
}

type FileTab = {
  id: string
  path: string
  name: string
  content: string
  language: string
  modified: boolean
}
```

### Reuse Strategy

Existing `TerminalTabs`, `TerminalView`, and `TopBar` components are reused as-is inside the IDE's `TerminalPanel`. Zero rewrite of terminal functionality.

---

## 6. Go Backend Structure (New Packages)

```
internal/
+-- config/         # extended: Mode, WorkspaceRoot fields
+-- server/         # extended: conditional routing based on Mode
+-- httpapi/        # extended: file API routes (guarded by Mode)
+-- filesystem/     # NEW: file operations, path validation, security
|   +-- fs.go       # tree listing, read, write, create, delete
|   +-- search.go   # grep-like search implementation
|   +-- security.go # path traversal protection, symlink policy
+-- auth/           # unchanged
+-- shells/         # unchanged
+-- terminal/       # extended: CWD support in session creation
```

---

## Non-Goals (YAGNI)

- Git integration (status, diff, commit) — not in v1
- LSP / IntelliSense — Monaco provides basic syntax highlighting only
- Collaborative editing — single user
- Extensions / plugin system
- File watching / live reload
- Integrated debugger

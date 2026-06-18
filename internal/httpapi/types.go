package httpapi

import "github.com/brainplusplus/9ed/internal/terminal"

type createSessionRequest struct {
	ShellID string `json:"shellId"`
	CWD     string `json:"cwd,omitempty"`
}

type createSessionResponse struct {
	ID      string                `json:"id"`
	Profile terminal.ShellProfile `json:"profile"`
}

type wsInboundMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

type wsOutboundMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
}

type writeFileRequest struct {
	Content string `json:"content"`
	// IfMatch optionally enforces optimistic concurrency: when non-empty, the
	// write is rejected with 409 Conflict unless the on-disk file currently
	// has this version. Pass the Version field returned by GET (or "new" to
	// require a non-existent destination).
	IfMatch string `json:"ifMatch,omitempty"`
}

type writeFileResponse struct {
	Version string `json:"version"`
}

type createEntryRequest struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type renameRequest struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

type copyMoveRequest struct {
	SourcePath string `json:"sourcePath"`
	DestPath   string `json:"destPath"`
}

type configResponse struct {
	WorkspaceRoot      string `json:"workspaceRoot"`
	TerminalAIMaxLines int    `json:"terminalAiMaxLines"`
	TunnelURL          string `json:"tunnelUrl,omitempty"`
}

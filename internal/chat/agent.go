package chat

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go-webttyd/internal/chat/acp"
	"go-webttyd/internal/chat/acpinstall"
)

// ChatEvent represents a streaming event from an agent session.
type ChatEvent struct {
	Type string `json:"type"`

	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	ToolCallID string `json:"toolCallId,omitempty"`
	ToolTitle  string `json:"toolTitle,omitempty"`
	ToolKind   string `json:"toolKind,omitempty"`
	ToolStatus string `json:"toolStatus,omitempty"`
	ToolContent string `json:"toolContent,omitempty"`
	ToolLocations []ToolLocation `json:"toolLocations,omitempty"`
	DiffPath    string `json:"diffPath,omitempty"`
	DiffOldText string `json:"diffOldText,omitempty"`
	DiffNewText string `json:"diffNewText,omitempty"`
	PlanEntries []PlanEntry `json:"planEntries,omitempty"`
	Commands []CommandInfo `json:"commands,omitempty"`
	ConfigOptions []ConfigOptionInfo `json:"configOptions,omitempty"`
	Title string `json:"title,omitempty"`
	StopReason string `json:"stopReason,omitempty"`
	Error string `json:"error,omitempty"`

	PermissionID      string             `json:"permissionId,omitempty"`
	PermissionTitle   string             `json:"permissionTitle,omitempty"`
	PermissionOptions []PermissionOption `json:"permissionOptions,omitempty"`
}

type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type CommandInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputHint   string `json:"inputHint,omitempty"`
}

type ConfigOptionInfo struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Category     string             `json:"category,omitempty"`
	Type         string             `json:"type"`
	CurrentValue string             `json:"currentValue"`
	Options      []ConfigValueInfo  `json:"options"`
}

type ConfigValueInfo struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type ToolLocation struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

type ChatSession interface {
	ID() string
	AgentID() string
	WorkDir() string
	Send(ctx context.Context, message string) error
	SetConfigOption(ctx context.Context, configID, value string) error
	Events() <-chan ChatEvent
	Cancel() error
	Close() error
	Done() <-chan struct{}
	Mode() SessionMode
	RespondPermission(resp PermissionResponse)
	SetAutoApprove(enabled bool)
	ACPSessionID() string
	IsResumed() bool
}

// SessionMode distinguishes ACP from PTY sessions.
type SessionMode string

const (
	ModeACP SessionMode = "acp"
	ModePTY SessionMode = "pty"
)

// AgentDescriptor holds metadata about a discovered agent and how to connect.
type AgentDescriptor struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	Command        string   `json:"command"`
	Args           []string `json:"args"`
	ACPCommand     string   `json:"acpCommand,omitempty"`
	ACPArgs        []string `json:"acpArgs,omitempty"`
	Available      bool     `json:"available"`
	SupportsACP    bool     `json:"supportsAcp"`
	ACPInstallable bool     `json:"acpInstallable,omitempty"`
}

// NewChatSession creates a ChatSession using ACP if supported, falling back to PTY.
func NewChatSession(ctx context.Context, agent AgentDescriptor, workDir string) (ChatSession, error) {
	if agent.SupportsACP {
		sess, err := newACPSession(ctx, agent, workDir)
		if err == nil {
			return sess, nil
		}
	}

	if !agent.SupportsACP && agent.ACPInstallable {
		if path, err := acpinstall.EnsureInstalled(agent.ID); err == nil {
			installed := agent
			installed.SupportsACP = true
			installed.ACPCommand = path
			info := acpinstall.GetAdapterInfo(agent.ID)
			if info != nil {
				installed.ACPArgs = []string{}
			}
			sess, err := newACPSession(ctx, installed, workDir)
			if err == nil {
				return sess, nil
			}
		}
	}

	return newPTYSession(agent, workDir)
}

type PermissionResponse struct {
	PermissionID string `json:"permissionId"`
	OptionID     string `json:"optionId"`
	Cancelled    bool   `json:"cancelled"`
}

type acpSession struct {
	id           string
	agentID      string
	workDir      string
	adapter      *acp.Adapter
	sessionID    string
	events       chan ChatEvent
	done         chan struct{}
	cancelFn     context.CancelFunc
	promptDone   chan *acp.SessionPromptResult
	textBuf      strings.Builder
	permissionCh chan PermissionResponse
	autoApprove  bool
	resumed      bool
}

func newACPSession(ctx context.Context, agent AgentDescriptor, workDir string) (*acpSession, error) {
	if workDir == "" {
		workDir = currentWorkingDirectory()
	}

	acpCtx, cancel := context.WithCancel(ctx)

	acpCommand := agent.Command
	if agent.ACPCommand != "" {
		acpCommand = agent.ACPCommand
	}

	cfg := acp.AdapterConfig{
		Command: acpCommand,
		Args:    agent.ACPArgs,
		WorkDir: workDir,
	}

	adapter, err := acp.NewAdapter(acpCtx, cfg)
	if err != nil {
		cancel()
		return nil, err
	}

	result, err := adapter.NewSession(acpCtx, workDir)
	if err != nil {
		_ = adapter.Close()
		cancel()
		return nil, err
	}

	s := &acpSession{
		id:           result.SessionID,
		agentID:      agent.ID,
		workDir:      workDir,
		adapter:      adapter,
		sessionID:    result.SessionID,
		events:       make(chan ChatEvent, 128),
		done:         make(chan struct{}),
		cancelFn:     cancel,
		promptDone:   make(chan *acp.SessionPromptResult, 1),
		permissionCh: make(chan PermissionResponse, 1),
	}

	if len(result.ConfigOptions) > 0 {
		s.events <- ChatEvent{Type: "config_options", ConfigOptions: convertConfigOptions(result.ConfigOptions)}
	}

	go s.processNotifications()
	return s, nil
}

func newACPResumedSession(ctx context.Context, agent AgentDescriptor, workDir, acpSessionID string) (*acpSession, error) {
	if workDir == "" {
		workDir = currentWorkingDirectory()
	}

	acpCtx, cancel := context.WithCancel(ctx)

	acpCommand := agent.Command
	if agent.ACPCommand != "" {
		acpCommand = agent.ACPCommand
	}

	cfg := acp.AdapterConfig{
		Command: acpCommand,
		Args:    agent.ACPArgs,
		WorkDir: workDir,
	}

	adapter, err := acp.NewAdapter(acpCtx, cfg)
	if err != nil {
		cancel()
		return nil, err
	}

	result, err := adapter.ResumeSession(acpCtx, acpSessionID, workDir)
	if err != nil {
		_ = adapter.Close()
		cancel()
		return nil, fmt.Errorf("session/resume failed: %w", err)
	}

	s := &acpSession{
		id:           result.SessionID,
		agentID:      agent.ID,
		workDir:      workDir,
		adapter:      adapter,
		sessionID:    result.SessionID,
		events:       make(chan ChatEvent, 128),
		done:         make(chan struct{}),
		cancelFn:     cancel,
		promptDone:   make(chan *acp.SessionPromptResult, 1),
		permissionCh: make(chan PermissionResponse, 1),
		resumed:      true,
	}

	if len(result.ConfigOptions) > 0 {
		s.events <- ChatEvent{Type: "config_options", ConfigOptions: convertConfigOptions(result.ConfigOptions)}
	}

	go s.processNotifications()
	return s, nil
}

func (s *acpSession) flushText() {
	if s.textBuf.Len() > 0 {
		s.events <- ChatEvent{Type: "text", Text: s.textBuf.String()}
		s.textBuf.Reset()
	}
}

func (s *acpSession) ID() string          { return s.id }
func (s *acpSession) AgentID() string     { return s.agentID }
func (s *acpSession) WorkDir() string     { return s.workDir }
func (s *acpSession) Mode() SessionMode   { return ModeACP }
func (s *acpSession) Events() <-chan ChatEvent { return s.events }
func (s *acpSession) Done() <-chan struct{}    { return s.done }
func (s *acpSession) ACPSessionID() string     { return s.sessionID }
func (s *acpSession) IsResumed() bool          { return s.resumed }

func (s *acpSession) RespondPermission(resp PermissionResponse) {
	select {
	case s.permissionCh <- resp:
	default:
	}
}

func (s *acpSession) SetAutoApprove(enabled bool) {
	s.autoApprove = enabled
}

func (s *acpSession) SetConfigOption(ctx context.Context, configID, value string) error {
	opts, err := s.adapter.SetConfigOption(ctx, s.sessionID, configID, value)
	if err != nil {
		return err
	}
	s.events <- ChatEvent{Type: "config_options", ConfigOptions: convertConfigOptions(opts)}
	return nil
}

func (s *acpSession) Send(ctx context.Context, message string) error {
	content := []acp.ContentBlock{
		{Type: "text", Text: message},
	}

	go func() {
		result, err := s.adapter.Prompt(ctx, s.sessionID, content)
		if err != nil {
			s.promptDone <- nil
			s.events <- ChatEvent{Type: "error", Error: err.Error()}
			return
		}
		s.promptDone <- result
	}()

	return nil
}

func (s *acpSession) Cancel() error {
	return s.adapter.Cancel(s.sessionID)
}

func (s *acpSession) Close() error {
	s.cancelFn()
	return s.adapter.Close()
}

func (s *acpSession) processNotifications() {
	defer close(s.done)

	flushTicker := time.NewTicker(50 * time.Millisecond)
	defer flushTicker.Stop()

	for {
		select {
		case notif, ok := <-s.adapter.Notifications():
			if !ok {
				s.flushText()
				return
			}
			s.handleNotification(notif)
		case req, ok := <-s.adapter.Requests():
			if !ok {
				s.flushText()
				return
			}
			s.handleRequest(req)
		case <-flushTicker.C:
			s.flushText()
		case result := <-s.promptDone:
			for drained := false; !drained; {
				select {
				case notif := <-s.adapter.Notifications():
					s.handleNotification(notif)
				case req := <-s.adapter.Requests():
					s.handleRequest(req)
				default:
					drained = true
				}
			}
			s.flushText()
			if result != nil {
				s.events <- ChatEvent{Type: "done", StopReason: string(result.StopReason)}
			}
		case <-s.adapter.Done():
			s.flushText()
			return
		}
	}
}

func (s *acpSession) handleNotification(notif *acp.Notification) {
	if notif.Method != acp.MethodSessionUpdate {
		return
	}

	var params acp.SessionUpdateParams
	if err := jsonUnmarshal(notif.Params, &params); err != nil {
		return
	}

	var base acp.SessionUpdateBase
	if err := jsonUnmarshal(params.Update, &base); err != nil {
		return
	}

	log.Printf("[ACP] session_update: %s", base.SessionUpdate)

	if base.SessionUpdate != acp.UpdateAgentMessageChunk {
		s.flushText()
	}

	switch base.SessionUpdate {
	case acp.UpdateAgentMessageChunk:
		var update acp.AgentMessageChunkUpdate
		if jsonUnmarshal(params.Update, &update) == nil {
			s.textBuf.WriteString(update.Content.Text)
		}
		return

	case acp.UpdateThoughtChunk:
		var update acp.AgentMessageChunkUpdate
		if jsonUnmarshal(params.Update, &update) == nil {
			s.events <- ChatEvent{Type: "thinking", Thinking: update.Content.Text}
		}

	case acp.UpdateToolCall:
		var update acp.ToolCallUpdate
		if jsonUnmarshal(params.Update, &update) == nil {
			evt := ChatEvent{
				Type:       "tool_call",
				ToolCallID: update.ToolCallID,
				ToolTitle:  update.Title,
				ToolKind:   string(update.Kind),
				ToolStatus: string(update.Status),
			}
			if len(update.Locations) > 0 {
				locs := make([]ToolLocation, len(update.Locations))
				for i, l := range update.Locations {
					locs[i] = ToolLocation{Path: l.Path, Line: l.Line}
				}
				evt.ToolLocations = locs
			}
			s.events <- evt
		}

	case acp.UpdateToolCallUpdate:
		var update acp.ToolCallStatusUpdate
		if jsonUnmarshal(params.Update, &update) == nil {
			var contentParts []string
			for _, c := range update.Content {
				switch c.Type {
				case "content":
					if c.Content != nil && c.Content.Text != "" {
						contentParts = append(contentParts, c.Content.Text)
					}
				case "diff":
					s.events <- ChatEvent{
						Type:        "diff",
						DiffPath:    c.Path,
						DiffOldText: c.OldText,
						DiffNewText: c.NewText,
					}
				case "terminal":
					if c.TerminalID != "" {
						contentParts = append(contentParts, "[terminal: "+c.TerminalID+"]")
					}
				}
			}
			evt := ChatEvent{
				Type:       "tool_call_update",
				ToolCallID: update.ToolCallID,
				ToolTitle:  update.Title,
				ToolStatus: string(update.Status),
			}
			if len(contentParts) > 0 {
				evt.ToolContent = strings.Join(contentParts, "\n")
			}
			s.events <- evt
		}

	case acp.UpdatePlan:
		var update acp.PlanUpdate
		if jsonUnmarshal(params.Update, &update) == nil {
			entries := make([]PlanEntry, len(update.Entries))
			for i, e := range update.Entries {
				entries[i] = PlanEntry{
					Content:  e.Content,
					Priority: e.Priority,
					Status:   e.Status,
				}
			}
			s.events <- ChatEvent{Type: "plan", PlanEntries: entries}
		}

	case acp.UpdateAvailableCommandsUpdate:
		var update acp.AvailableCommandsUpdate
		if jsonUnmarshal(params.Update, &update) == nil {
			cmds := make([]CommandInfo, len(update.AvailableCommands))
			for i, c := range update.AvailableCommands {
				cmds[i] = CommandInfo{Name: c.Name, Description: c.Description}
				if c.Input != nil {
					cmds[i].InputHint = c.Input.Hint
				}
			}
			s.events <- ChatEvent{Type: "commands", Commands: cmds}
		}

	case acp.UpdateConfigOptionsUpdate:
		var update acp.ConfigOptionsUpdate
		if jsonUnmarshal(params.Update, &update) == nil {
			s.events <- ChatEvent{Type: "config_options", ConfigOptions: convertConfigOptions(update.ConfigOptions)}
		}

	case acp.UpdateSessionInfoUpdate:
		var update acp.SessionInfoUpdateNotification
		if jsonUnmarshal(params.Update, &update) == nil && update.Title != "" {
			s.events <- ChatEvent{Type: "title", Title: update.Title}
		}
	}
}

func (s *acpSession) handleRequest(req *acp.Request) {
	switch req.Method {
	case acp.MethodRequestPermission:
		var params acp.RequestPermissionParams
		if jsonUnmarshal(req.Params, &params) != nil {
			return
		}

		if s.autoApprove {
			optionID := ""
			for _, opt := range params.Options {
				if opt.Kind == acp.PermissionAllowOnce {
					optionID = opt.OptionID
					break
				}
			}
			if optionID == "" {
				for _, opt := range params.Options {
					if opt.Kind == acp.PermissionAllowAlways {
						optionID = opt.OptionID
						break
					}
				}
			}
			if optionID == "" && len(params.Options) > 0 {
				optionID = params.Options[0].OptionID
			}
			_ = s.adapter.Respond(req.ID, acp.RequestPermissionResult{
				Outcome: acp.PermissionOutcome{Outcome: "selected", OptionID: optionID},
			}, nil)
			return
		}

		permID := fmt.Sprintf("perm_%d", req.ID)
		title := ""
		if params.ToolCall.Title != "" {
			title = params.ToolCall.Title
		}

		options := make([]PermissionOption, len(params.Options))
		for i, opt := range params.Options {
			options[i] = PermissionOption{
				OptionID: opt.OptionID,
				Name:     opt.Name,
				Kind:     string(opt.Kind),
			}
		}

		s.events <- ChatEvent{
			Type:              "permission_request",
			PermissionID:      permID,
			PermissionTitle:   title,
			PermissionOptions: options,
			ToolCallID:        params.ToolCall.ToolCallID,
			ToolKind:          string(params.ToolCall.Kind),
		}

		resp := <-s.permissionCh

		if resp.Cancelled {
			_ = s.adapter.Respond(req.ID, acp.RequestPermissionResult{
				Outcome: acp.PermissionOutcome{Outcome: "cancelled"},
			}, nil)
		} else {
			_ = s.adapter.Respond(req.ID, acp.RequestPermissionResult{
				Outcome: acp.PermissionOutcome{
					Outcome:  "selected",
					OptionID: resp.OptionID,
				},
			}, nil)
		}

	case acp.MethodFSReadTextFile:
		var params acp.FSReadTextFileParams
		if jsonUnmarshal(req.Params, &params) != nil {
			_ = s.adapter.Respond(req.ID, nil, &acp.RPCError{
				Code:    -32602,
				Message: "invalid params",
			})
			return
		}
		content, err := readFileContent(params.Path)
		if err != nil {
			_ = s.adapter.Respond(req.ID, nil, &acp.RPCError{
				Code:    -32002,
				Message: err.Error(),
			})
			return
		}
		_ = s.adapter.Respond(req.ID, acp.FSReadTextFileResult{Text: content}, nil)

	case acp.MethodFSWriteTextFile:
		var params acp.FSWriteTextFileParams
		if jsonUnmarshal(req.Params, &params) != nil {
			_ = s.adapter.Respond(req.ID, nil, &acp.RPCError{
				Code:    -32602,
				Message: "invalid params",
			})
			return
		}
		if err := writeFileContent(params.Path, params.Text); err != nil {
			_ = s.adapter.Respond(req.ID, nil, &acp.RPCError{
				Code:    -32603,
				Message: err.Error(),
			})
			return
		}
		_ = s.adapter.Respond(req.ID, nil, nil)

	default:
		_ = s.adapter.Respond(req.ID, nil, &acp.RPCError{
			Code:    -32601,
			Message: "method not supported: " + req.Method,
		})
	}
}

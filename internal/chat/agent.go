package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/brainplusplus/9ed/internal/chat/acp"
	"github.com/brainplusplus/9ed/internal/chat/acpinstall"
	"github.com/brainplusplus/9ed/internal/debug"
)

// ChatEvent represents a streaming event from an agent session.
type ChatEvent struct {
	Type string `json:"type"`

	Text          string             `json:"text,omitempty"`
	Thinking      string             `json:"thinking,omitempty"`
	ToolCallID    string             `json:"toolCallId,omitempty"`
	ToolTitle     string             `json:"toolTitle,omitempty"`
	ToolKind      string             `json:"toolKind,omitempty"`
	ToolStatus    string             `json:"toolStatus,omitempty"`
	ToolContent   string             `json:"toolContent,omitempty"`
	ToolRawInput  string             `json:"toolRawInput,omitempty"`
	ToolLocations []ToolLocation     `json:"toolLocations,omitempty"`
	DiffPath      string             `json:"diffPath,omitempty"`
	DiffOldText   string             `json:"diffOldText,omitempty"`
	DiffNewText   string             `json:"diffNewText,omitempty"`
	PlanEntries   []PlanEntry        `json:"planEntries,omitempty"`
	Commands      []CommandInfo      `json:"commands,omitempty"`
	ConfigOptions []ConfigOptionInfo `json:"configOptions,omitempty"`
	Title         string             `json:"title,omitempty"`
	StopReason    string             `json:"stopReason,omitempty"`
	Error         string             `json:"error,omitempty"`

	ContextWindow int     `json:"contextWindow,omitempty"`
	ContextUsed   int     `json:"contextUsed,omitempty"`
	CostAmount    float64 `json:"costAmount,omitempty"`
	CostCurrency  string  `json:"costCurrency,omitempty"`

	PermissionID      string             `json:"permissionId,omitempty"`
	PermissionTitle   string             `json:"permissionTitle,omitempty"`
	PermissionOptions []PermissionOption `json:"permissionOptions,omitempty"`

	TerminalCommand string `json:"terminalCommand,omitempty"`
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
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	Category     string            `json:"category,omitempty"`
	Type         string            `json:"type"`
	CurrentValue string            `json:"currentValue"`
	Options      []ConfigValueInfo `json:"options"`
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
	Send(ctx context.Context, message string, attachments []Attachment) error
	SetConfigOption(ctx context.Context, configID, value string) error
	Events() <-chan ChatEvent
	Cancel() error
	Close() error
	Done() <-chan struct{}
	Mode() SessionMode
	RespondPermission(resp PermissionResponse)
	SetAutoApprove(enabled bool)
	SetUseActiveTerminal(enabled bool, terminalID string)
	UseActiveTerminalEnabled() bool
	ActiveTerminalID() string
	SetUseActiveBrowser(enabled bool, tabID string)
	UseActiveBrowserEnabled() bool
	ActiveBrowserTabID() string
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

var activeTerminalMCPServers []acp.MCPServer
var activeBrowserMCPServers []acp.MCPServer

func SetActiveTerminalMCPServers(servers []acp.MCPServer) {
	activeTerminalMCPServers = servers
}

func SetActiveBrowserMCPServers(servers []acp.MCPServer) {
	activeBrowserMCPServers = servers
}

type SessionOptions struct {
	UseActiveTerminal  bool
	ActiveTerminalID   string
	UseActiveBrowser   bool
	ActiveBrowserTabID string
}

func activeMCPServersForOptions(opts SessionOptions) []acp.MCPServer {
	servers := make([]acp.MCPServer, 0, len(activeTerminalMCPServers)+len(activeBrowserMCPServers))
	if opts.UseActiveTerminal {
		servers = append(servers, activeTerminalMCPServers...)
	}
	// Keep browser MCP tools always registered so runtime browser toggle does not
	// produce "invalid tool" errors on already-connected ACP sessions.
	servers = append(servers, activeBrowserMCPServers...)
	return servers
}

// NewChatSession creates a ChatSession using ACP if supported, falling back to PTY.
func NewChatSession(ctx context.Context, agent AgentDescriptor, workDir string, opts SessionOptions) (ChatSession, error) {
	if agent.SupportsACP {
		sess, err := newACPSession(ctx, agent, workDir, opts)
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
			sess, err := newACPSession(ctx, installed, workDir, opts)
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

type acpTerminal struct {
	id      string
	command string
}

type promptCompletion struct {
	turnID uint64
	result *acp.SessionPromptResult
}

type acpToolMeta struct {
	title    string
	kind     acp.ToolKind
	rawInput json.RawMessage
}

type acpSession struct {
	id                 string
	agentID            string
	workDir            string
	adapter            *acp.Adapter
	sessionID          string
	ctx                context.Context
	events             chan ChatEvent
	done               chan struct{}
	cancelFn           context.CancelFunc
	promptDone         chan promptCompletion
	turnMu             sync.Mutex
	nextTurnID         uint64
	activeTurnID       uint64
	completedTurns     map[uint64]bool
	recoveredTurns     map[uint64]bool
	toolRecoveryTimer  *time.Timer
	textBuf            strings.Builder
	permissionCh       chan PermissionResponse
	autoApprove        bool
	useActiveTerminal  bool
	activeTerminalID   string
	useActiveBrowser   bool
	activeBrowserTabID string
	resumed            bool
	terminals          map[string]*acpTerminal
	routedToolCalls    map[string]bool
	toolMeta           map[string]acpToolMeta
}

func newACPSession(ctx context.Context, agent AgentDescriptor, workDir string, opts SessionOptions) (*acpSession, error) {
	if workDir == "" {
		workDir = currentWorkingDirectory()
	}

	acpCtx, cancel := context.WithCancel(ctx)

	acpCommand := agent.Command
	if agent.ACPCommand != "" {
		acpCommand = agent.ACPCommand
	}

	cfg := acp.AdapterConfig{
		Command:    acpCommand,
		Args:       agent.ACPArgs,
		WorkDir:    workDir,
		MCPServers: activeMCPServersForOptions(opts),
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
		id:                 result.SessionID,
		agentID:            agent.ID,
		workDir:            workDir,
		adapter:            adapter,
		sessionID:          result.SessionID,
		ctx:                acpCtx,
		events:             make(chan ChatEvent, 128),
		done:               make(chan struct{}),
		cancelFn:           cancel,
		promptDone:         make(chan promptCompletion, 1),
		completedTurns:     make(map[uint64]bool),
		recoveredTurns:     make(map[uint64]bool),
		permissionCh:       make(chan PermissionResponse, 1),
		useActiveTerminal:  opts.UseActiveTerminal,
		activeTerminalID:   opts.ActiveTerminalID,
		useActiveBrowser:   opts.UseActiveBrowser,
		activeBrowserTabID: opts.ActiveBrowserTabID,
		terminals:          make(map[string]*acpTerminal),
		routedToolCalls:    make(map[string]bool),
		toolMeta:           make(map[string]acpToolMeta),
	}

	if len(result.ConfigOptions) > 0 {
		s.events <- ChatEvent{Type: "config_options", ConfigOptions: convertConfigOptions(result.ConfigOptions)}
	}

	go s.processNotifications()
	return s, nil
}

func newACPResumedSession(ctx context.Context, agent AgentDescriptor, workDir, acpSessionID string, opts SessionOptions) (*acpSession, error) {
	if workDir == "" {
		workDir = currentWorkingDirectory()
	}

	acpCtx, cancel := context.WithCancel(ctx)

	acpCommand := agent.Command
	if agent.ACPCommand != "" {
		acpCommand = agent.ACPCommand
	}

	cfg := acp.AdapterConfig{
		Command:    acpCommand,
		Args:       agent.ACPArgs,
		WorkDir:    workDir,
		MCPServers: activeMCPServersForOptions(opts),
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
		id:                 result.SessionID,
		agentID:            agent.ID,
		workDir:            workDir,
		adapter:            adapter,
		sessionID:          result.SessionID,
		ctx:                acpCtx,
		events:             make(chan ChatEvent, 128),
		done:               make(chan struct{}),
		cancelFn:           cancel,
		promptDone:         make(chan promptCompletion, 1),
		completedTurns:     make(map[uint64]bool),
		recoveredTurns:     make(map[uint64]bool),
		permissionCh:       make(chan PermissionResponse, 1),
		useActiveTerminal:  opts.UseActiveTerminal,
		activeTerminalID:   opts.ActiveTerminalID,
		useActiveBrowser:   opts.UseActiveBrowser,
		activeBrowserTabID: opts.ActiveBrowserTabID,
		resumed:            true,
		terminals:          make(map[string]*acpTerminal),
		routedToolCalls:    make(map[string]bool),
		toolMeta:           make(map[string]acpToolMeta),
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

func (s *acpSession) ID() string               { return s.id }
func (s *acpSession) AgentID() string          { return s.agentID }
func (s *acpSession) WorkDir() string          { return s.workDir }
func (s *acpSession) Mode() SessionMode        { return ModeACP }
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

func (s *acpSession) SetUseActiveTerminal(enabled bool, terminalID string) {
	s.useActiveTerminal = enabled
	s.activeTerminalID = strings.TrimSpace(terminalID)
}

func (s *acpSession) UseActiveTerminalEnabled() bool {
	return s.useActiveTerminal
}

func (s *acpSession) ActiveTerminalID() string {
	return s.activeTerminalID
}

func (s *acpSession) SetUseActiveBrowser(enabled bool, tabID string) {
	s.useActiveBrowser = enabled
	s.activeBrowserTabID = strings.TrimSpace(tabID)
}

func (s *acpSession) UseActiveBrowserEnabled() bool {
	return s.useActiveBrowser
}

func (s *acpSession) ActiveBrowserTabID() string {
	return s.activeBrowserTabID
}

func (s *acpSession) SetConfigOption(ctx context.Context, configID, value string) error {
	opts, err := s.adapter.SetConfigOption(ctx, s.sessionID, configID, value)
	if err != nil {
		return err
	}
	s.events <- ChatEvent{Type: "config_options", ConfigOptions: convertConfigOptions(opts)}
	return nil
}

func (s *acpSession) Send(_ context.Context, message string, attachments []Attachment) error {
	turnID := s.beginPromptTurn()
	imageCapable := s.adapter.AgentCapabilities().PromptCapabilities != nil && s.adapter.AgentCapabilities().PromptCapabilities.Image
	if s.useActiveBrowser || s.useActiveTerminal {
		message = decorateActiveToolMessage(message, s.useActiveBrowser, s.useActiveTerminal)
	}
	content := buildACPContentBlocks(message, attachments, imageCapable)
	if s.useActiveBrowser {
		debug.BrowserMCPLog("agent", "info", "prompt start session=%s tab=%s turn=%d chars=%d attachments=%d", s.sessionID, s.activeBrowserTabID, turnID, len(message), len(attachments))
	}

	go func() {
		result, err := s.adapter.Prompt(s.ctx, s.sessionID, content)
		if s.isTurnCompleted(turnID) {
			return
		}
		completion := promptCompletion{turnID: turnID, result: result}
		if err != nil {
			if s.useActiveBrowser {
				debug.BrowserMCPLog("agent", "error", "prompt error session=%s tab=%s turn=%d err=%v", s.sessionID, s.activeBrowserTabID, turnID, err)
			}
			s.enqueuePromptDone(completion)
			s.events <- ChatEvent{Type: "error", Error: err.Error()}
			return
		}
		if s.useActiveBrowser {
			debug.BrowserMCPLog("agent", "info", "prompt result session=%s tab=%s turn=%d stop=%s", s.sessionID, s.activeBrowserTabID, turnID, result.StopReason)
			debug.BrowserMCPLog("agent", "info", "prompt enqueue session=%s tab=%s turn=%d buffered=%d", s.sessionID, s.activeBrowserTabID, turnID, len(s.promptDone))
		}
		s.enqueuePromptDone(completion)
	}()

	return nil
}

func decorateActiveToolMessage(message string, includeBrowser bool, includeTerminal bool) string {
	message = strings.TrimSpace(message)
	guide := activeToolPromptGuide(includeBrowser, includeTerminal)
	if message == "" {
		return guide
	}
	return message + "\n\n" + guide
}

func activeToolPromptGuide(includeBrowser bool, includeTerminal bool) string {
	lines := []string{"Active tool workflow guidance:"}
	if includeBrowser {
		lines = append(lines,
			"- Treat browser MCP as a hands-on workflow tool for debugging, pairing, and completing tasks in the active page.",
			"- When the workflow requires page interaction, use 9ed_browser_click/type/press for the needed button, CTA, link, form, or control. This applies whether the need comes from the user's words or from your own analysis of the page state.",
			"- Use 9ed_browser_inspect, 9ed_browser_page_source, console logs, and network requests to gather evidence and choose targeted browser actions.",
			"- Do not use 9ed_browser_screenshot just to decide what to click; reserve screenshots for visual proof, layout/UI verification, or image analysis.",
		)
	}
	if includeTerminal {
		lines = append(lines,
			"- Treat terminal MCP as a workflow tool for build, test, logs, diagnostics, and local commands needed to complete the current task.",
			"- Chain terminal commands when the last result reveals the next necessary diagnostic or fix step; avoid redundant confirmation commands when the result already proves the point.",
			"- Use active_terminal_start for long-running servers/watchers, active_terminal_read to observe them, and active_terminal_run for commands expected to finish.",
		)
	}
	lines = append(lines, "- Answer when the current task is satisfied; otherwise continue with the next smallest useful browser or terminal action.")
	return strings.Join(lines, "\n")
}

func (s *acpSession) beginPromptTurn() uint64 {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	s.cancelToolRecoveryLocked()
	s.nextTurnID++
	turnID := s.nextTurnID
	s.activeTurnID = turnID
	delete(s.completedTurns, turnID)
	delete(s.recoveredTurns, turnID)
	return turnID
}

func (s *acpSession) isTurnCompleted(turnID uint64) bool {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	return s.completedTurns[turnID]
}

func (s *acpSession) cancelToolRecoveryLocked() {
	if s.toolRecoveryTimer != nil {
		s.toolRecoveryTimer.Stop()
		s.toolRecoveryTimer = nil
	}
}

func (s *acpSession) clearToolRecovery() {
	s.turnMu.Lock()
	s.cancelToolRecoveryLocked()
	s.turnMu.Unlock()
}

func (s *acpSession) scheduleToolRecovery(title string) {
	if !isInteractiveMCPToolTitle(title) {
		return
	}
	if isBrowserToolCallTitle(title) {
		return
	}
	s.turnMu.Lock()
	turnID := s.activeTurnID
	if turnID == 0 || s.completedTurns[turnID] {
		s.turnMu.Unlock()
		return
	}
	s.cancelToolRecoveryLocked()
	s.toolRecoveryTimer = time.AfterFunc(2500*time.Millisecond, func() {
		s.turnMu.Lock()
		if s.activeTurnID != turnID || s.completedTurns[turnID] {
			s.turnMu.Unlock()
			return
		}
		s.completedTurns[turnID] = true
		s.recoveredTurns[turnID] = true
		s.activeTurnID = 0
		s.cancelToolRecoveryLocked()
		s.turnMu.Unlock()

		debug.Printf("[ACP] recovering stalled turn after completed MCP tool: session=%s turn=%d title=%q", s.sessionID, turnID, title)
		_ = s.adapter.Cancel(s.sessionID)
		s.events <- ChatEvent{Type: "done", StopReason: "tool_completion_timeout"}
	})
	s.turnMu.Unlock()
}

func (s *acpSession) enqueuePromptDone(completion promptCompletion) {
	select {
	case s.promptDone <- completion:
		if s.useActiveBrowser {
			debug.BrowserMCPLog("agent", "info", "prompt enqueued session=%s tab=%s turn=%d", s.sessionID, s.activeBrowserTabID, completion.turnID)
		}
	case <-time.After(2 * time.Second):
		if s.useActiveBrowser {
			debug.BrowserMCPLog("agent", "error", "prompt done fallback session=%s tab=%s turn=%d", s.sessionID, s.activeBrowserTabID, completion.turnID)
		}
		s.emitPromptDone(completion)
	}
}

func (s *acpSession) Cancel() error {
	if s.useActiveBrowser {
		debug.BrowserMCPLog("agent", "info", "cancel requested session=%s tab=%s", s.sessionID, s.activeBrowserTabID)
	}
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
		case completion := <-s.promptDone:
			if s.useActiveBrowser {
				debug.BrowserMCPLog("agent", "info", "prompt received session=%s tab=%s turn=%d stop=%s", s.sessionID, s.activeBrowserTabID, completion.turnID, func() string {
					if completion.result == nil {
						return "<nil>"
					}
					return string(completion.result.StopReason)
				}())
			}
			for drained := false; !drained; {
				select {
				case notif, ok := <-s.adapter.Notifications():
					if ok && notif != nil {
						s.handleNotification(notif)
					}
				case req, ok := <-s.adapter.Requests():
					if ok && req != nil {
						s.handleRequest(req)
					}
				default:
					drained = true
				}
			}
			s.flushText()
			s.emitPromptDone(completion)
		case <-s.adapter.Done():
			s.flushText()
			completion := <-s.promptDone
			if s.useActiveBrowser {
				debug.BrowserMCPLog("agent", "info", "adapter done path received pending prompt session=%s tab=%s turn=%d stop=%s", s.sessionID, s.activeBrowserTabID, completion.turnID, func() string {
					if completion.result == nil {
						return "<nil>"
					}
					return string(completion.result.StopReason)
				}())
			}
			s.emitPromptDone(completion)
			if s.useActiveBrowser {
				debug.BrowserMCPLog("agent", "error", "adapter done session=%s tab=%s err=%v", s.sessionID, s.activeBrowserTabID, s.adapter.Err())
			}
			return
		}
	}
}

func (s *acpSession) emitPromptDone(completion promptCompletion) {
	if completion.result == nil {
		if s.useActiveBrowser {
			debug.BrowserMCPLog("agent", "error", "prompt completed without result session=%s tab=%s turn=%d", s.sessionID, s.activeBrowserTabID, completion.turnID)
		}
		return
	}

	s.turnMu.Lock()
	if s.completedTurns[completion.turnID] {
		s.turnMu.Unlock()
		if s.useActiveBrowser {
			debug.BrowserMCPLog("agent", "info", "done duplicate ignored session=%s tab=%s turn=%d stop=%s", s.sessionID, s.activeBrowserTabID, completion.turnID, completion.result.StopReason)
		}
		return
	}
	s.completedTurns[completion.turnID] = true
	if s.activeTurnID == completion.turnID {
		s.activeTurnID = 0
	}
	s.cancelToolRecoveryLocked()
	s.turnMu.Unlock()

	if s.useActiveBrowser {
		debug.BrowserMCPLog("agent", "info", "done emitted session=%s tab=%s turn=%d stop=%s", s.sessionID, s.activeBrowserTabID, completion.turnID, completion.result.StopReason)
	}
	s.events <- ChatEvent{Type: "done", StopReason: string(completion.result.StopReason)}
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

	debug.Printf("[ACP] session_update: %s raw: %.500s", base.SessionUpdate, string(params.Update))

	if base.SessionUpdate != acp.UpdateAgentMessageChunk {
		s.flushText()
	}

	switch base.SessionUpdate {
	case acp.UpdateAgentMessageChunk:
		s.clearToolRecovery()
		var update acp.AgentMessageChunkUpdate
		if jsonUnmarshal(params.Update, &update) == nil {
			s.textBuf.WriteString(update.Content.Text)
		}
		return

	case acp.UpdateThoughtChunk:
		s.clearToolRecovery()
		var update acp.AgentMessageChunkUpdate
		if jsonUnmarshal(params.Update, &update) == nil {
			s.events <- ChatEvent{Type: "thinking", Thinking: update.Content.Text}
		}

	case acp.UpdateToolCall:
		s.clearToolRecovery()
		var update acp.ToolCallUpdate
		if jsonUnmarshal(params.Update, &update) == nil {
			s.rememberToolMeta(update.ToolCallID, update.Title, update.Kind, update.RawInput)
			if s.useActiveBrowser && isBrowserToolCallTitle(update.Title) {
				debug.BrowserMCPLog("agent", "info", "tool call session=%s tab=%s id=%s title=%s status=%s input=%s", s.sessionID, s.activeBrowserTabID, update.ToolCallID, update.Title, update.Status, summarizeToolRawJSON(update.RawInput))
				// Browser tool events are bridged by /api/chat/browser/run as the
				// canonical stream source to avoid duplicated tool cards.
				return
			}
			if s.redirectToolCallToActiveTerminal(update) {
				return
			}
			evt := ChatEvent{
				Type:         "tool_call",
				ToolCallID:   update.ToolCallID,
				ToolTitle:    update.Title,
				ToolKind:     string(update.Kind),
				ToolStatus:   string(update.Status),
				ToolRawInput: string(update.RawInput),
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
			s.applyRememberedToolMeta(&update)
			if s.useActiveBrowser && isBrowserToolCallTitle(update.Title) {
				debug.BrowserMCPLog("agent", "info", "tool update session=%s tab=%s id=%s title=%s status=%s input=%s", s.sessionID, s.activeBrowserTabID, update.ToolCallID, update.Title, update.Status, summarizeToolRawJSON(update.RawInput))
				s.clearToolRecovery()
				if update.Status == acp.ToolStatusCompleted || update.Status == acp.ToolStatusFailed {
					delete(s.toolMeta, update.ToolCallID)
				}
				return
			}
			if update.Status != acp.ToolStatusCompleted && update.Status != acp.ToolStatusFailed {
				tool := acp.ToolCallUpdate{
					ToolCallID: update.ToolCallID,
					Title:      update.Title,
					Kind:       update.Kind,
					Status:     update.Status,
					RawInput:   update.RawInput,
				}
				if s.redirectToolCallToActiveTerminal(tool) {
					return
				}
			}
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
				Type:         "tool_call_update",
				ToolCallID:   update.ToolCallID,
				ToolTitle:    update.Title,
				ToolStatus:   string(update.Status),
				ToolRawInput: string(update.RawInput),
			}
			if len(contentParts) > 0 {
				evt.ToolContent = strings.Join(contentParts, "\n")
			}
			s.events <- evt
			if update.Status == acp.ToolStatusCompleted || update.Status == acp.ToolStatusFailed {
				delete(s.toolMeta, update.ToolCallID)
				s.scheduleToolRecovery(update.Title)
			} else {
				s.clearToolRecovery()
			}
		}

	case acp.UpdatePlan:
		s.clearToolRecovery()
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
		if jsonUnmarshal(params.Update, &update) == nil {
			ctxWindow := update.ContextWindow
			ctxUsed := update.ContextUsed
			// Fallback: try alternate field locations
			if ctxWindow == 0 && update.TokenUsage.ContextWindow > 0 {
				ctxWindow = update.TokenUsage.ContextWindow
			}
			if ctxUsed == 0 && update.TokenUsage.TotalTokens > 0 {
				ctxUsed = update.TokenUsage.TotalTokens
			}
			// Also try reading raw JSON for any context-related fields
			if ctxWindow == 0 || ctxUsed == 0 {
				var raw map[string]any
				if jsonUnmarshal(params.Update, &raw) == nil {
					if ctxUsed == 0 {
						if v, ok := raw["totalTokens"]; ok {
							if f, ok := toFloat(v); ok && f > 0 {
								ctxUsed = int(f)
							}
						}
						if v, ok := raw["tokensUsed"]; ok {
							if f, ok := toFloat(v); ok && f > 0 {
								ctxUsed = int(f)
							}
						}
					}
					if ctxWindow == 0 {
						if v, ok := raw["maxTokens"]; ok {
							if f, ok := toFloat(v); ok && f > 0 {
								ctxWindow = int(f)
							}
						}
					}
				}
			}
			debug.Printf("[ACP] session_info: title=%q contextWindow=%d contextUsed=%d", update.Title, ctxWindow, ctxUsed)
			evt := ChatEvent{Type: "session_info", ContextWindow: ctxWindow, ContextUsed: ctxUsed}
			if update.Title != "" {
				evt.Title = update.Title
			}
			s.events <- evt
		}

	case acp.UpdateUsageUpdate:
		var update acp.UsageUpdateNotification
		if jsonUnmarshal(params.Update, &update) == nil {
			debug.Printf("[ACP] usage_update: used=%d size=%d cost=%.4f%s", update.Used, update.Size, update.Cost.Amount, update.Cost.Currency)
			s.events <- ChatEvent{
				Type:          "session_info",
				ContextWindow: update.Size,
				ContextUsed:   update.Used,
				CostAmount:    update.Cost.Amount,
				CostCurrency:  update.Cost.Currency,
			}
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

		if s.useActiveTerminal {
			if cmd := commandFromToolCall(params.ToolCall); cmd != "" {
				debug.Printf("[ACP] redirect execute permission to active terminal: %q", cmd)
				s.events <- ChatEvent{
					Type:            "terminal_execute",
					TerminalCommand: cmd,
					ToolCallID:      params.ToolCall.ToolCallID,
					ToolTitle:       params.ToolCall.Title,
					ToolKind:        string(params.ToolCall.Kind),
					ToolRawInput:    string(params.ToolCall.RawInput),
				}
				_ = s.adapter.Respond(req.ID, acp.RequestPermissionResult{
					Outcome: acp.PermissionOutcome{Outcome: "cancelled"},
				}, nil)
				return
			}
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

		go s.waitForPermissionResponse(req.ID)

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
				Code:    -32003,
				Message: err.Error(),
			})
			return
		}
		_ = s.adapter.Respond(req.ID, nil, nil)

	// --- Terminal methods: redirect commands to user's active terminal ---
	case acp.MethodTerminalCreate:
		var params acp.TerminalCreateParams
		if jsonUnmarshal(req.Params, &params) != nil {
			_ = s.adapter.Respond(req.ID, nil, &acp.RPCError{
				Code:    -32602,
				Message: "invalid params",
			})
			return
		}
		// Build full command string
		cmd := params.Command
		if len(params.Args) > 0 {
			cmd = cmd + " " + strings.Join(params.Args, " ")
		}
		termID := fmt.Sprintf("term_%d", req.ID)
		s.terminals[termID] = &acpTerminal{id: termID, command: cmd}

		// Emit event to frontend so it can type the command into the user's terminal
		if cmd != "" {
			debug.Printf("[ACP] terminal/create → redirect to user terminal: %q", cmd)
			s.events <- ChatEvent{
				Type:            "terminal_execute",
				TerminalCommand: cmd,
			}
		}

		// Return a fake terminal ID — the AI will use it for output/release
		_ = s.adapter.Respond(req.ID, acp.TerminalCreateResult{TerminalID: termID}, nil)

	case acp.MethodTerminalOutput:
		// AI asks for terminal output — we don't have the real output since
		// it ran in the user's terminal, not ours. Return empty.
		// The AI gets the actual command output via its internal tool result.
		_ = s.adapter.Respond(req.ID, acp.TerminalOutputResult{Output: ""}, nil)

	case acp.MethodTerminalRelease:
		var params struct {
			TerminalID string `json:"terminalId"`
		}
		if jsonUnmarshal(req.Params, &params) == nil {
			delete(s.terminals, params.TerminalID)
		}
		_ = s.adapter.Respond(req.ID, nil, nil)

	case acp.MethodTerminalWaitExit:
		// Pretend the terminal exited successfully immediately
		exitCode := 0
		_ = s.adapter.Respond(req.ID, acp.TerminalOutputResult{ExitCode: &exitCode}, nil)

	case acp.MethodTerminalKill:
		var params struct {
			TerminalID string `json:"terminalId"`
		}
		if jsonUnmarshal(req.Params, &params) == nil {
			delete(s.terminals, params.TerminalID)
		}
		_ = s.adapter.Respond(req.ID, nil, nil)

	default:
		_ = s.adapter.Respond(req.ID, nil, &acp.RPCError{
			Code:    -32601,
			Message: "method not supported: " + req.Method,
		})
	}
}

func (s *acpSession) waitForPermissionResponse(requestID int64) {
	resp := <-s.permissionCh

	if resp.Cancelled {
		_ = s.adapter.Respond(requestID, acp.RequestPermissionResult{
			Outcome: acp.PermissionOutcome{Outcome: "cancelled"},
		}, nil)
		return
	}

	_ = s.adapter.Respond(requestID, acp.RequestPermissionResult{
		Outcome: acp.PermissionOutcome{
			Outcome:  "selected",
			OptionID: resp.OptionID,
		},
	}, nil)
}

func commandFromToolCall(tool acp.ToolCallUpdate) string {
	kind := strings.ToLower(string(tool.Kind))
	if kind != string(acp.ToolKindExecute) && kind != "shell" && kind != "bash" && kind != "powershell" && kind != "pwsh" && kind != "cmd" {
		return ""
	}
	return commandFromRawInput(tool.RawInput)
}

func isBrowserToolCallTitle(title string) bool {
	value := strings.TrimSpace(strings.ToLower(title))
	return strings.HasPrefix(value, "9ed_browser_") ||
		strings.HasPrefix(value, "9ed-active-browser_") ||
		strings.HasPrefix(value, "active_browser_") ||
		strings.HasPrefix(value, "browser_")
}

func (s *acpSession) rememberToolMeta(toolCallID, title string, kind acp.ToolKind, rawInput json.RawMessage) {
	if strings.TrimSpace(toolCallID) == "" {
		return
	}
	meta := s.toolMeta[toolCallID]
	if strings.TrimSpace(title) != "" {
		meta.title = title
	}
	if strings.TrimSpace(string(kind)) != "" {
		meta.kind = kind
	}
	if len(rawInput) > 0 {
		meta.rawInput = append(json.RawMessage(nil), rawInput...)
	}
	s.toolMeta[toolCallID] = meta
}

func (s *acpSession) applyRememberedToolMeta(update *acp.ToolCallStatusUpdate) {
	if update == nil || strings.TrimSpace(update.ToolCallID) == "" {
		return
	}
	s.rememberToolMeta(update.ToolCallID, update.Title, update.Kind, update.RawInput)
	meta, ok := s.toolMeta[update.ToolCallID]
	if !ok {
		return
	}
	if strings.TrimSpace(update.Title) == "" {
		update.Title = meta.title
	}
	if strings.TrimSpace(string(update.Kind)) == "" {
		update.Kind = meta.kind
	}
	if len(update.RawInput) == 0 && len(meta.rawInput) > 0 {
		update.RawInput = append(json.RawMessage(nil), meta.rawInput...)
	}
}

func isInteractiveMCPToolTitle(title string) bool {
	value := strings.TrimSpace(strings.ToLower(title))
	return value == "active_terminal_run" ||
		value == "active_terminal_start" ||
		value == "active_terminal_read" ||
		isBrowserToolCallTitle(title)
}

func summarizeToolRawJSON(raw json.RawMessage) string {
	value := strings.Join(strings.Fields(strings.TrimSpace(string(raw))), " ")
	if value == "" {
		return "-"
	}
	const maxLen = 180
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}

func (s *acpSession) redirectToolCallToActiveTerminal(tool acp.ToolCallUpdate) bool {
	if !s.useActiveTerminal || tool.ToolCallID == "" || s.routedToolCalls[tool.ToolCallID] {
		return false
	}
	if tool.Status == acp.ToolStatusCompleted || tool.Status == acp.ToolStatusFailed {
		return false
	}
	cmd := commandFromToolCall(tool)
	if cmd == "" {
		return false
	}
	s.routedToolCalls[tool.ToolCallID] = true
	debug.Printf("[ACP] redirect tool call to active terminal and cancel turn: id=%s command=%q", tool.ToolCallID, cmd)
	s.events <- ChatEvent{
		Type:            "terminal_execute",
		TerminalCommand: cmd,
		ToolCallID:      tool.ToolCallID,
		ToolTitle:       tool.Title,
		ToolKind:        string(tool.Kind),
		ToolRawInput:    string(tool.RawInput),
	}
	s.events <- ChatEvent{Type: "done", StopReason: "terminal_command_sent"}
	_ = s.adapter.Cancel(s.sessionID)
	return true
}

func commandFromRawInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}

	for _, key := range []string{"command", "cmd", "script", "code", "input"} {
		if value, ok := obj[key].(string); ok && strings.TrimSpace(value) != "" {
			cmd := strings.TrimSpace(value)
			if args, ok := obj["args"].([]any); ok && len(args) > 0 {
				var parts []string
				parts = append(parts, cmd)
				for _, arg := range args {
					parts = append(parts, quoteCommandArg(fmt.Sprint(arg)))
				}
				return strings.Join(parts, " ")
			}
			return cmd
		}
	}

	return ""
}

func quoteCommandArg(arg string) string {
	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(arg, `\`, `\\`), `"`, `\"`) + `"`
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

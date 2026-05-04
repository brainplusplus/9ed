package acp

import "encoding/json"

// --- JSON-RPC 2.0 Base Types ---

// Request represents a JSON-RPC 2.0 request message.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response message.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Notification represents a JSON-RPC 2.0 notification (no id field).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return e.Message
}

// --- Initialize ---

// InitializeParams is sent by the client to begin the ACP handshake.
type InitializeParams struct {
	ProtocolVersion    int                 `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities  `json:"clientCapabilities"`
	ClientInfo         *ImplementationInfo `json:"clientInfo,omitempty"`
}

// InitializeResult is the agent's response to the initialize request.
type InitializeResult struct {
	ProtocolVersion   int                 `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities   `json:"agentCapabilities"`
	AgentInfo         *ImplementationInfo `json:"agentInfo,omitempty"`
	AuthMethods       []AuthMethod        `json:"authMethods,omitempty"`
}

// ClientCapabilities describes what the client supports.
type ClientCapabilities struct {
	FS       *FSCapabilities `json:"fs,omitempty"`
	Terminal bool            `json:"terminal,omitempty"`
}

// FSCapabilities describes file system capabilities.
type FSCapabilities struct {
	ReadTextFile  bool `json:"readTextFile,omitempty"`
	WriteTextFile bool `json:"writeTextFile,omitempty"`
}

// AgentCapabilities describes what the agent supports.
type AgentCapabilities struct {
	LoadSession         bool                 `json:"loadSession,omitempty"`
	PromptCapabilities  *PromptCapabilities  `json:"promptCapabilities,omitempty"`
	MCPCapabilities     *MCPCapabilities     `json:"mcpCapabilities,omitempty"`
	SessionCapabilities *SessionCapabilities `json:"sessionCapabilities,omitempty"`
}

// PromptCapabilities describes content types the agent accepts in prompts.
type PromptCapabilities struct {
	Image           bool `json:"image,omitempty"`
	Audio           bool `json:"audio,omitempty"`
	EmbeddedContext bool `json:"embeddedContext,omitempty"`
}

// MCPCapabilities describes MCP transport support.
type MCPCapabilities struct {
	HTTP bool `json:"http,omitempty"`
	SSE  bool `json:"sse,omitempty"`
}

// SessionCapabilities describes optional session methods.
type SessionCapabilities struct {
	Resume *struct{} `json:"resume,omitempty"`
	Close  *struct{} `json:"close,omitempty"`
}

// ImplementationInfo identifies a client or agent implementation.
type ImplementationInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// AuthMethod describes an authentication method offered by the agent.
type AuthMethod struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

// --- Session Setup ---

// SessionNewParams creates a new conversation session.
type SessionNewParams struct {
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers"`
}

// SessionNewResult is the response to session/new.
type SessionNewResult struct {
	SessionID     string              `json:"sessionId"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
	Modes         *SessionModeState   `json:"modes,omitempty"`
}

// SessionConfigOption represents a dynamic config selector (model, mode, etc).
type SessionConfigOption struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	Category     string              `json:"category,omitempty"`
	Type         string              `json:"type"`
	CurrentValue string              `json:"currentValue"`
	Options      []ConfigOptionValue `json:"options"`
}

// ConfigOptionValue is one selectable value in a config option.
type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionModeState describes available modes and the current one.
type SessionModeState struct {
	CurrentModeID  string        `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

// SessionMode describes a single agent mode.
type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SetConfigOptionParams changes a config option value.
type SetConfigOptionParams struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

// SetConfigOptionResult returns the updated full config state.
type SetConfigOptionResult struct {
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

// SessionLoadParams loads an existing session.
type SessionLoadParams struct {
	SessionID  string      `json:"sessionId"`
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers,omitempty"`
}

// SessionResumeParams resumes an existing session without replay.
type SessionResumeParams struct {
	SessionID  string      `json:"sessionId"`
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers,omitempty"`
}

// SessionCloseParams closes an active session.
type SessionCloseParams struct {
	SessionID string `json:"sessionId"`
}

// MCPServer describes an MCP server to connect to.
type MCPServer struct {
	Name    string        `json:"name"`
	Command string        `json:"command,omitempty"`
	Args    []string      `json:"args,omitempty"`
	Env     []EnvVariable `json:"env,omitempty"`
	// HTTP transport fields
	Type    string       `json:"type,omitempty"` // "http" or "sse"
	URL     string       `json:"url,omitempty"`
	Headers []HTTPHeader `json:"headers,omitempty"`
}

// EnvVariable is a name-value pair for environment variables.
type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HTTPHeader is a name-value pair for HTTP headers.
type HTTPHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// --- Prompt Turn ---

// SessionPromptParams sends a user message to the agent.
type SessionPromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// SessionPromptResult is the response when the prompt turn ends.
type SessionPromptResult struct {
	StopReason StopReason `json:"stopReason"`
}

// StopReason indicates why the agent stopped.
type StopReason string

const (
	StopReasonEndTurn         StopReason = "end_turn"
	StopReasonMaxTokens       StopReason = "max_tokens"
	StopReasonMaxTurnRequests StopReason = "max_turn_requests"
	StopReasonRefusal         StopReason = "refusal"
	StopReasonCancelled       StopReason = "cancelled"
)

// SessionCancelParams cancels an ongoing prompt turn (notification, no response).
type SessionCancelParams struct {
	SessionID string `json:"sessionId"`
}

// --- Session Update (Notification from Agent → Client) ---

// SessionUpdateParams is the notification payload for session/update.
type SessionUpdateParams struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

// SessionUpdateType identifies the kind of update.
type SessionUpdateType string

const (
	UpdateAgentMessageChunk      SessionUpdateType = "agent_message_chunk"
	UpdateUserMessageChunk       SessionUpdateType = "user_message_chunk"
	UpdateThoughtChunk           SessionUpdateType = "agent_thought_chunk"
	UpdateToolCall               SessionUpdateType = "tool_call"
	UpdateToolCallUpdate         SessionUpdateType = "tool_call_update"
	UpdatePlan                   SessionUpdateType = "plan"
	UpdateSessionInfoUpdate      SessionUpdateType = "session_info_update"
	UpdateAvailableCommandsUpdate SessionUpdateType = "available_commands_update"
	UpdateConfigOptionsUpdate     SessionUpdateType = "config_options_update"
)

// AvailableCommandsUpdate reports slash commands the agent supports.
type AvailableCommandsUpdate struct {
	SessionUpdate     SessionUpdateType  `json:"sessionUpdate"`
	AvailableCommands []AvailableCommand `json:"availableCommands"`
}

// AvailableCommand describes a slash command available in the agent.
type AvailableCommand struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Input       *AvailableCommandInput `json:"input,omitempty"`
}

// AvailableCommandInput describes the input hint for a command.
type AvailableCommandInput struct {
	Hint string `json:"hint"`
}

// ConfigOptionsUpdate is sent by the agent when config options change.
type ConfigOptionsUpdate struct {
	SessionUpdate SessionUpdateType     `json:"sessionUpdate"`
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

// SessionUpdateBase contains the discriminator field.
type SessionUpdateBase struct {
	SessionUpdate SessionUpdateType `json:"sessionUpdate"`
}

// AgentMessageChunkUpdate is a streaming text chunk from the agent.
type AgentMessageChunkUpdate struct {
	SessionUpdate SessionUpdateType `json:"sessionUpdate"` // "agent_message_chunk"
	Content       ContentBlock      `json:"content"`
}

// UserMessageChunkUpdate replays a user message (during session/load).
type UserMessageChunkUpdate struct {
	SessionUpdate SessionUpdateType `json:"sessionUpdate"` // "user_message_chunk"
	Content       ContentBlock      `json:"content"`
}

// ToolCallUpdate reports a new tool call.
type ToolCallUpdate struct {
	SessionUpdate SessionUpdateType `json:"sessionUpdate"` // "tool_call"
	ToolCallID    string            `json:"toolCallId"`
	Title         string            `json:"title"`
	Kind          ToolKind          `json:"kind,omitempty"`
	Status        ToolCallStatus    `json:"status"`
	Content       []ToolCallContent `json:"content,omitempty"`
	Locations     []ToolCallLocation `json:"locations,omitempty"`
	RawInput      json.RawMessage   `json:"rawInput,omitempty"`
	RawOutput     json.RawMessage   `json:"rawOutput,omitempty"`
}

// ToolCallStatusUpdate reports progress on an existing tool call.
type ToolCallStatusUpdate struct {
	SessionUpdate SessionUpdateType `json:"sessionUpdate"` // "tool_call_update"
	ToolCallID    string            `json:"toolCallId"`
	Status        ToolCallStatus    `json:"status,omitempty"`
	Title         string            `json:"title,omitempty"`
	Content       []ToolCallContent `json:"content,omitempty"`
	Locations     []ToolCallLocation `json:"locations,omitempty"`
	RawInput      json.RawMessage   `json:"rawInput,omitempty"`
	RawOutput     json.RawMessage   `json:"rawOutput,omitempty"`
}

// PlanUpdate reports the agent's execution plan.
type PlanUpdate struct {
	SessionUpdate SessionUpdateType `json:"sessionUpdate"` // "plan"
	Entries       []PlanEntry       `json:"entries"`
}

// PlanEntry is a single step in the agent's plan.
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

// --- Tool Call Types ---

// ToolKind categorizes the type of tool being invoked.
type ToolKind string

const (
	ToolKindRead    ToolKind = "read"
	ToolKindEdit    ToolKind = "edit"
	ToolKindDelete  ToolKind = "delete"
	ToolKindMove    ToolKind = "move"
	ToolKindSearch  ToolKind = "search"
	ToolKindExecute ToolKind = "execute"
	ToolKindThink   ToolKind = "think"
	ToolKindFetch   ToolKind = "fetch"
	ToolKindOther   ToolKind = "other"
)

// ToolCallStatus represents the execution state of a tool call.
type ToolCallStatus string

const (
	ToolStatusPending    ToolCallStatus = "pending"
	ToolStatusInProgress ToolCallStatus = "in_progress"
	ToolStatusCompleted  ToolCallStatus = "completed"
	ToolStatusFailed     ToolCallStatus = "failed"
)

// ToolCallContent wraps content produced by a tool call.
type ToolCallContent struct {
	Type    string       `json:"type"` // "content", "diff", "terminal"
	Content *ContentBlock `json:"content,omitempty"`
	// Diff fields
	Path    string `json:"path,omitempty"`
	OldText string `json:"oldText,omitempty"`
	NewText string `json:"newText,omitempty"`
	// Terminal fields
	TerminalID string `json:"terminalId,omitempty"`
}

// ToolCallLocation identifies a file location affected by a tool call.
type ToolCallLocation struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// --- Permission Request (Agent → Client method call) ---

// RequestPermissionParams is sent by the agent to request user approval.
type RequestPermissionParams struct {
	SessionID string           `json:"sessionId"`
	ToolCall  ToolCallUpdate   `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// RequestPermissionResult is the client's response.
type RequestPermissionResult struct {
	Outcome PermissionOutcome `json:"outcome"`
}

// PermissionOption describes a choice presented to the user.
type PermissionOption struct {
	OptionID string             `json:"optionId"`
	Name     string             `json:"name"`
	Kind     PermissionKind     `json:"kind"`
}

// PermissionKind categorizes the permission option.
type PermissionKind string

const (
	PermissionAllowOnce   PermissionKind = "allow_once"
	PermissionAllowAlways PermissionKind = "allow_always"
	PermissionRejectOnce  PermissionKind = "reject_once"
	PermissionRejectAlways PermissionKind = "reject_always"
)

// PermissionOutcome represents the user's decision.
type PermissionOutcome struct {
	Outcome  string `json:"outcome"` // "selected" or "cancelled"
	OptionID string `json:"optionId,omitempty"`
}

// --- Content Blocks ---

// ContentBlock represents a piece of displayable content.
// Discriminated by the Type field.
type ContentBlock struct {
	Type string `json:"type"` // "text", "image", "audio", "resource", "resource_link"

	// Text content
	Text string `json:"text,omitempty"`

	// Image/Audio content
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`

	// Resource content (embedded)
	Resource *EmbeddedResource `json:"resource,omitempty"`

	// Resource link
	URI         string `json:"uri,omitempty"`
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

// EmbeddedResource contains the full content of a resource.
type EmbeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// --- File System Methods (Client → Agent responses) ---

// FSReadTextFileParams is sent by the agent to read a file.
type FSReadTextFileParams struct {
	Path string `json:"path"`
}

// FSReadTextFileResult is the client's response with file content.
type FSReadTextFileResult struct {
	Text string `json:"text"`
}

// FSWriteTextFileParams is sent by the agent to write a file.
type FSWriteTextFileParams struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// --- Terminal Methods (Client provides to Agent) ---

// TerminalCreateParams creates a new terminal for the agent.
type TerminalCreateParams struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	CWD     string   `json:"cwd,omitempty"`
}

// TerminalCreateResult returns the terminal ID.
type TerminalCreateResult struct {
	TerminalID string `json:"terminalId"`
}

// TerminalOutputParams requests terminal output.
type TerminalOutputParams struct {
	TerminalID string `json:"terminalId"`
}

// TerminalOutputResult contains terminal output and exit status.
type TerminalOutputResult struct {
	Output   string `json:"output"`
	ExitCode *int   `json:"exitCode,omitempty"`
}

// --- Session Info Update ---

// SessionInfoUpdateNotification provides metadata about the session.
type SessionInfoUpdateNotification struct {
	SessionUpdate SessionUpdateType `json:"sessionUpdate"` // "session_info_update"
	Title         string            `json:"title,omitempty"`
	Model         string            `json:"model,omitempty"`
}

// --- ACP Method Names ---

const (
	MethodInitialize        = "initialize"
	MethodAuthenticate      = "authenticate"
	MethodSessionNew        = "session/new"
	MethodSessionLoad       = "session/load"
	MethodSessionResume     = "session/resume"
	MethodSessionClose      = "session/close"
	MethodSessionPrompt     = "session/prompt"
	MethodSessionCancel          = "session/cancel"
	MethodSessionUpdate          = "session/update"
	MethodSessionSetConfigOption = "session/set_config_option"
	MethodSessionSetMode         = "session/set_mode"
	MethodRequestPermission      = "session/request_permission"
	MethodFSReadTextFile    = "fs/read_text_file"
	MethodFSWriteTextFile   = "fs/write_text_file"
	MethodTerminalCreate    = "terminal/create"
	MethodTerminalOutput    = "terminal/output"
	MethodTerminalRelease   = "terminal/release"
	MethodTerminalWaitExit  = "terminal/wait_for_exit"
	MethodTerminalKill      = "terminal/kill"
)

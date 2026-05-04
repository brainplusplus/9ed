package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go-webttyd/internal/chat"
)

type chatCreateRequest struct {
	AgentID string `json:"agentId"`
}

type chatCreateResponse struct {
	ID string `json:"id"`
}

type chatSessionInfo struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`
}

type chatWSInbound struct {
	Type    string          `json:"type"`
	Content string          `json:"content,omitempty"`
	Context json.RawMessage `json:"context,omitempty"`
}

type chatWSOutbound struct {
	Type         string `json:"type"`
	Content      string `json:"content,omitempty"`
	Message      string `json:"message,omitempty"`
	Action       string `json:"action,omitempty"`
	Detail       string `json:"detail,omitempty"`
	NewSessionID string `json:"newSessionId,omitempty"`
}

func (a *API) handleChatAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	agents := chat.DiscoverAgents()
	writeJSON(w, http.StatusOK, agents)
}

func (a *API) handleChatSessions(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		sessions := a.chatManager.List()
		infos := make([]chatSessionInfo, 0, len(sessions))
		for _, s := range sessions {
			infos = append(infos, chatSessionInfo{ID: s.ID, AgentID: s.AgentID})
		}
		writeJSON(w, http.StatusOK, infos)

	case http.MethodPost:
		var req chatCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		agent, ok := findAgent(req.AgentID)
		if !ok {
			http.Error(w, "unknown agent", http.StatusBadRequest)
			return
		}

		session, err := a.chatManager.Create(agent)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusCreated, chatCreateResponse{ID: session.ID})

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *API) handleChatSessionByID(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/chat/sessions/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodDelete {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	a.chatManager.Remove(id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleChatWebSocket(w http.ResponseWriter, r *http.Request) {
	if a.chatManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/ws/chat/")
	session, ok := a.chatManager.Get(sessionID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var writeMu sync.Mutex
	sendMsg := func(msg chatWSOutbound) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.WriteJSON(msg)
	}

	sendError := func(message string) {
		sendMsg(chatWSOutbound{Type: "error", Message: message})
	}

	stopOutput := make(chan struct{})
	go func() {
		streamTimer := time.NewTimer(500 * time.Millisecond)
		streamTimer.Stop()
		streaming := false

		for {
			select {
			case <-stopOutput:
				return
			case data, ok := <-session.Output():
				if !ok {
					if streaming {
						sendMsg(chatWSOutbound{Type: "stream_end"})
					}
					return
				}
				if data == "" {
					continue
				}
				streaming = true
				streamTimer.Reset(500 * time.Millisecond)
				parsed := chat.StripANSI(data)
				if parsed != "" {
					sendMsg(chatWSOutbound{Type: "stream", Content: parsed})
				}
			case <-streamTimer.C:
				if streaming {
					sendMsg(chatWSOutbound{Type: "stream_end"})
					streaming = false
				}
			}
		}
	}()

	defer close(stopOutput)

	for {
		var msg chatWSInbound
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		switch msg.Type {
		case "message":
			if err := session.Write(msg.Content + "\n"); err != nil {
				sendError(err.Error())
				return
			}

		case "message_with_context":
			formatted := formatContextMessage(msg.Content, msg.Context)
			if err := session.Write(formatted + "\n"); err != nil {
				sendError(err.Error())
				return
			}

		case "new_chat":
			newSession, err := session.Reset()
			if err != nil {
				sendError(fmt.Sprintf("reset failed: %v", err))
				continue
			}
			a.chatManager.ReplaceSession(sessionID, newSession)
			session = newSession
			sessionID = newSession.ID
			sendMsg(chatWSOutbound{Type: "session_reset", NewSessionID: newSession.ID})

		case "cancel":
			if err := session.Interrupt(); err != nil {
				sendError(err.Error())
			}

		default:
			sendError("unsupported message type")
		}
	}
}

type chatHistoryMessageRequest struct {
	SessionID    string `json:"sessionId"`
	AgentID      string `json:"agentId"`
	Title        string `json:"title"`
	Role         string `json:"role"`
	Content      string `json:"content"`
	ContextFile  string `json:"contextFile,omitempty"`
	ContextStart int    `json:"contextStartLine,omitempty"`
	ContextEnd   int    `json:"contextEndLine,omitempty"`
	ContextCode  string `json:"contextCode,omitempty"`
	ContextLang  string `json:"contextLanguage,omitempty"`
}

func (a *API) handleChatHistory(w http.ResponseWriter, r *http.Request) {
	if a.chatStore == nil {
		http.Error(w, "chat history not available", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		sessions, err := a.chatStore.ListSessions(50)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if sessions == nil {
			sessions = []chat.SessionRecord{}
		}
		writeJSON(w, http.StatusOK, sessions)

	case http.MethodPost:
		var req chatHistoryMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.SessionID == "" || req.Role == "" || req.Content == "" {
			http.Error(w, "sessionId, role, and content are required", http.StatusBadRequest)
			return
		}

		sessions, _ := a.chatStore.ListSessions(0)
		sessionExists := false
		for _, s := range sessions {
			if s.ID == req.SessionID {
				sessionExists = true
				break
			}
		}

		if !sessionExists {
			agentId := req.AgentID
			if agentId == "" {
				agentId = "unknown"
			}
			title := req.Title
			if title == "" {
				title = truncate(req.Content, 50)
			}
			if err := a.chatStore.CreateSession(req.SessionID, agentId, title); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		now := time.Now().UnixMilli()
		msgID := fmt.Sprintf("%s-%d", req.SessionID, now)
		msg := chat.MessageRecord{
			ID:           msgID,
			SessionID:    req.SessionID,
			Role:         req.Role,
			Content:      req.Content,
			ContextFile:  req.ContextFile,
			ContextStart: req.ContextStart,
			ContextEnd:   req.ContextEnd,
			ContextCode:  req.ContextCode,
			ContextLang:  req.ContextLang,
			Timestamp:    now,
		}
		if err := a.chatStore.AddMessage(msg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *API) handleChatHistoryByID(w http.ResponseWriter, r *http.Request) {
	if a.chatStore == nil {
		http.Error(w, "chat history not available", http.StatusServiceUnavailable)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/chat/history/")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		messages, err := a.chatStore.GetMessages(sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if messages == nil {
			messages = []chat.MessageRecord{}
		}
		writeJSON(w, http.StatusOK, messages)

	case http.MethodDelete:
		if err := a.chatStore.DeleteSession(sessionID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func findAgent(id string) (chat.Agent, bool) {
	agents := chat.DiscoverAgents()
	for _, a := range agents {
		if a.ID == id && a.Available {
			return a, true
		}
	}
	return chat.Agent{}, false
}

func formatContextMessage(content string, ctx json.RawMessage) string {
	if ctx == nil || len(ctx) == 0 {
		return content
	}

	var context struct {
		FilePath     string `json:"filePath"`
		StartLine    int    `json:"startLine"`
		EndLine      int    `json:"endLine"`
		SelectedCode string `json:"selectedCode"`
		Language     string `json:"language"`
	}

	if err := json.Unmarshal(ctx, &context); err != nil {
		return content
	}

	var sb strings.Builder
	if context.FilePath != "" {
		sb.WriteString(fmt.Sprintf("File: %s", context.FilePath))
		if context.StartLine > 0 {
			sb.WriteString(fmt.Sprintf(" (lines %d-%d)", context.StartLine, context.EndLine))
		}
		sb.WriteString("\n")
	}
	if context.SelectedCode != "" {
		sb.WriteString("```")
		if context.Language != "" {
			sb.WriteString(context.Language)
		}
		sb.WriteString("\n")
		sb.WriteString(context.SelectedCode)
		sb.WriteString("\n```\n\n")
	}
	sb.WriteString(content)
	return sb.String()
}

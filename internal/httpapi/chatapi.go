package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/brainplusplus/9ed/internal/chat"
	"github.com/brainplusplus/9ed/internal/chat/agentconfig"
	"github.com/brainplusplus/9ed/internal/debug"
)

type chatCreateRequest struct {
	AgentID      string `json:"agentId"`
	WorkDir      string `json:"workDir,omitempty"`
	ResumeID     string `json:"resumeId,omitempty"`
	ACPSessionID string `json:"acpSessionId,omitempty"`
}

type chatCreateResponse struct {
	ID           string `json:"id"`
	Mode         string `json:"mode"`
	IsResumed    bool   `json:"isResumed"`
	ResumedFrom  string `json:"resumedFrom,omitempty"`
	WorkDir      string `json:"workDir,omitempty"`
	ACPSessionID string `json:"acpSessionId,omitempty"`
}

type chatSessionInfo struct {
	ID           string `json:"id"`
	AgentID      string `json:"agentId"`
	Mode         string `json:"mode"`
	WorkDir      string `json:"workDir,omitempty"`
	ACPSessionID string `json:"acpSessionId,omitempty"`
	IsResumed    bool   `json:"isResumed"`
}

type chatRestoreResponse struct {
	Found            bool   `json:"found"`
	SessionID        string `json:"sessionId,omitempty"`
	LiveSessionID    string `json:"liveSessionId,omitempty"`
	AgentID          string `json:"agentId,omitempty"`
	WorkDir          string `json:"workDir,omitempty"`
	ACPSessionID     string `json:"acpSessionId,omitempty"`
	Status           string `json:"status,omitempty"`
	Title            string `json:"title,omitempty"`
	IsLive           bool   `json:"isLive"`
	CanResume        bool   `json:"canResume"`
	AgentSupportsACP bool   `json:"agentSupportsAcp,omitempty"`
	AgentAvailable   bool   `json:"agentAvailable,omitempty"`
	ResumeError      string `json:"resumeError,omitempty"`
}

type chatResumeRequest struct {
	SessionID    string `json:"sessionId"`
	AgentID      string `json:"agentId"`
	WorkDir      string `json:"workDir"`
	ACPSessionID string `json:"acpSessionId"`
}

var discoverAgentDescriptors = chat.DiscoverAgentDescriptors

type chatWSInbound struct {
	Type        string           `json:"type"`
	Content     string           `json:"content,omitempty"`
	Context     json.RawMessage  `json:"context,omitempty"`
	ConfigID    string           `json:"configId,omitempty"`
	Value       string           `json:"value,omitempty"`
	Attachments []chatAttachment `json:"attachments,omitempty"`

	PermissionID string `json:"permissionId,omitempty"`
	OptionID     string `json:"optionId,omitempty"`
	Cancelled    bool   `json:"cancelled,omitempty"`
	AutoApprove  bool   `json:"autoApprove,omitempty"`
}

type chatAttachment struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Name string `json:"name"`
}

func (a *API) handleChatAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	configs := agentconfig.DetectAll()
	writeJSON(w, http.StatusOK, configs)
}

func (a *API) handleChatSessions(w http.ResponseWriter, r *http.Request) {
	if a.chatSessionManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		sessions := a.chatSessionManager.List()
		infos := make([]chatSessionInfo, 0, len(sessions))
		for _, s := range sessions {
			infos = append(infos, chatSessionInfo{
				ID:           s.ID(),
				AgentID:      s.AgentID(),
				Mode:         string(s.Mode()),
				WorkDir:      s.WorkDir(),
				ACPSessionID: s.ACPSessionID(),
				IsResumed:    s.IsResumed(),
			})
		}
		writeJSON(w, http.StatusOK, infos)

	case http.MethodPost:
		var req chatCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		agent, ok := findAgentDescriptor(req.AgentID)
		if !ok {
			http.Error(w, "unknown or unavailable agent", http.StatusBadRequest)
			return
		}

		workDir := req.WorkDir
		if workDir == "" {
			workDir = a.workspaceRoot
		}

		var session chat.ChatSession
		var err error
		resumedFrom := ""

		if req.ResumeID != "" && req.ACPSessionID != "" && agent.SupportsACP {
			session, err = a.chatSessionManager.Resume(context.Background(), agent, workDir, req.ACPSessionID)
			if err != nil {
				session, err = a.chatSessionManager.Create(context.Background(), agent, workDir)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			} else {
				resumedFrom = req.ResumeID
			}
		} else {
			session, err = a.chatSessionManager.Create(context.Background(), agent, workDir)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if a.chatStore != nil {
			if resumedFrom != "" && session.ID() != req.ResumeID {
				_ = a.chatStore.UpdateSessionACP(req.ResumeID, session.ACPSessionID(), workDir)
				a.chatSessionManager.LinkRecordID(session.ID(), req.ResumeID)
			} else {
				record, _ := a.chatStore.GetSession(session.ID())
				if record == nil {
					_ = a.chatStore.CreateSessionFull(
						session.ID(),
						session.AgentID(),
						"",
						workDir,
						session.ACPSessionID(),
					)
				} else {
					_ = a.chatStore.UpdateSessionACP(session.ID(), session.ACPSessionID(), workDir)
				}
				a.chatSessionManager.LinkRecordID(session.ID(), session.ID())
			}
		}

		writeJSON(w, http.StatusCreated, chatCreateResponse{
			ID:           session.ID(),
			Mode:         string(session.Mode()),
			IsResumed:    resumedFrom != "",
			ResumedFrom:  resumedFrom,
			WorkDir:      session.WorkDir(),
			ACPSessionID: session.ACPSessionID(),
		})

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *API) handleChatSessionByID(w http.ResponseWriter, r *http.Request) {
	if a.chatSessionManager == nil {
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

	a.chatSessionManager.Remove(id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) newChatEventPersister(sessionID string) chatEventPersister {
	if a.chatStore == nil {
		debug.Printf("[chat/persist] disabled: no chat store session=%s", sessionID)
		return nil
	}

	persistRecordID := a.chatSessionManager.RecordIDFor(sessionID)
	if persistRecordID == sessionID {
		persistRecordID = a.chatStore.ResolveRecordID(sessionID)
	}
	if persistRecordID == "" {
		debug.Printf("[chat/persist] disabled: empty record id for live session=%s", sessionID)
		return nil
	}
	if record, err := a.chatStore.GetSession(persistRecordID); err != nil {
		debug.Printf("[chat/persist] record lookup failed live=%s record=%s err=%v", sessionID, persistRecordID, err)
	} else if record == nil {
		if live, ok := a.chatSessionManager.Get(sessionID); ok {
			if err := a.chatStore.CreateSessionFull(persistRecordID, live.AgentID(), "", live.WorkDir(), live.ACPSessionID()); err != nil {
				debug.Printf("[chat/persist] record create failed live=%s record=%s agent=%s workDir=%q err=%v", sessionID, persistRecordID, live.AgentID(), live.WorkDir(), err)
			} else {
				debug.Printf("[chat/persist] record created live=%s record=%s agent=%s workDir=%q", sessionID, persistRecordID, live.AgentID(), live.WorkDir())
			}
		} else {
			debug.Printf("[chat/persist] record missing and live session unavailable live=%s record=%s", sessionID, persistRecordID)
		}
	}
	debug.Printf("[chat/persist] attached live=%s record=%s", sessionID, persistRecordID)

	persistSeq, _ := a.chatStore.NextEventSeq(persistRecordID)
	var mu sync.Mutex
	var assistantText strings.Builder
	assistantMessageID := ""
	assistantSaved := false
	saveAssistantDraft := func() {
		if strings.TrimSpace(assistantText.String()) == "" {
			return
		}
		now := time.Now().UnixMilli()
		if assistantMessageID == "" {
			assistantMessageID = fmt.Sprintf("%s-assistant-%d", persistRecordID, now)
		}
		if err := a.chatStore.UpsertMessage(chat.MessageRecord{
			ID:        assistantMessageID,
			SessionID: persistRecordID,
			Role:      "assistant",
			Content:   assistantText.String(),
			Timestamp: now,
		}); err != nil {
			debug.Printf("[chat/persist] assistant upsert failed live=%s record=%s msg=%s chars=%d err=%v", sessionID, persistRecordID, assistantMessageID, assistantText.Len(), err)
		} else {
			debug.Printf("[chat/persist] assistant upserted live=%s record=%s msg=%s chars=%d", sessionID, persistRecordID, assistantMessageID, assistantText.Len())
		}
	}

	return func(evt chat.ChatEvent) {
		mu.Lock()
		defer mu.Unlock()
		if evt.Type == "text" || evt.Type == "done" || evt.Type == "error" || evt.Type == "usage_update" {
			debug.Printf("[chat/persist] event live=%s record=%s type=%s textChars=%d stop=%q err=%q", sessionID, persistRecordID, evt.Type, len(evt.Text), evt.StopReason, evt.Error)
		}

		switch evt.Type {
		case "text":
			if assistantSaved {
				assistantText.Reset()
				assistantMessageID = ""
				assistantSaved = false
			}
			assistantText.WriteString(evt.Text)
			saveAssistantDraft()
		case "done":
			if !assistantSaved {
				saveAssistantDraft()
				assistantSaved = true
			}
		}

		shouldPersist := false
		switch evt.Type {
		case "text", "thinking", "tool_call", "tool_call_update", "diff",
			"plan", "title", "done", "error", "session_info", "usage_update":
			shouldPersist = true
		case "commands":
			shouldPersist = true
			commandsJSON, _ := json.Marshal(evt.Commands)
			_ = a.chatStore.SaveSnapshot(chat.SessionSnapshot{
				SessionID:    persistRecordID,
				CommandsJSON: string(commandsJSON),
			})
		case "config_options":
			shouldPersist = true
			configJSON, _ := json.Marshal(evt.ConfigOptions)
			snap, _ := a.chatStore.GetSnapshot(persistRecordID)
			commandsJSON := ""
			if snap != nil {
				commandsJSON = snap.CommandsJSON
			}
			_ = a.chatStore.SaveSnapshot(chat.SessionSnapshot{
				SessionID:      persistRecordID,
				CommandsJSON:   commandsJSON,
				ConfigOptsJSON: string(configJSON),
			})
		}
		if !shouldPersist {
			return
		}
		payload, _ := json.Marshal(evt)
		now := time.Now().UnixMilli()
		for attempt := 0; attempt < 2; attempt++ {
			evtID := fmt.Sprintf("%s-e-%d", persistRecordID, persistSeq)
			record := chat.EventRecord{
				ID:          evtID,
				SessionID:   persistRecordID,
				Kind:        evt.Type,
				PayloadJSON: string(payload),
				Seq:         persistSeq,
				Timestamp:   now,
			}
			persistSeq++
			if err := a.chatStore.AppendEvent(record); err == nil {
				if evt.Type == "text" || evt.Type == "done" || evt.Type == "error" {
					debug.Printf("[chat/persist] event appended live=%s record=%s seq=%d type=%s", sessionID, persistRecordID, record.Seq, evt.Type)
				}
				break
			} else {
				debug.Printf("[chat/persist] event append failed live=%s record=%s seq=%d type=%s attempt=%d err=%v", sessionID, persistRecordID, record.Seq, evt.Type, attempt+1, err)
			}
			nextSeq, err := a.chatStore.NextEventSeq(persistRecordID)
			if err != nil {
				break
			}
			persistSeq = nextSeq
		}
		if evt.Title != "" && (evt.Type == "title" || evt.Type == "session_info") {
			_ = a.chatStore.UpdateSessionTitle(persistRecordID, evt.Title)
		}
	}
}

func (a *API) handleChatWebSocket(w http.ResponseWriter, r *http.Request) {
	if a.chatSessionManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/ws/chat/")
	session, ok := a.chatSessionManager.Get(sessionID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stream := a.chatStreams.GetOrCreate(sessionID, session, a.newChatEventPersister(sessionID))
	sub := stream.Subscribe()
	defer stream.Unsubscribe(sub)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-sub.C:
				if !ok {
					return
				}
				if err := conn.WriteJSON(evt); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	for {
		var msg chatWSInbound
		if err := conn.ReadJSON(&msg); err != nil {
			cancel()
			return
		}

		switch msg.Type {
		case "message":
			content := msg.Content
			if msg.Context != nil && len(msg.Context) > 0 {
				content = formatContextMessage(msg.Content, msg.Context)
			}
			if len(msg.Attachments) > 0 {
				content = formatAttachments(content, msg.Attachments)
			}
			if err := session.Send(ctx, content); err != nil {
				stream.publish(chat.ChatEvent{Type: "error", Error: err.Error()})
			}

		case "cancel":
			if err := session.Cancel(); err != nil {
				stream.publish(chat.ChatEvent{Type: "error", Error: err.Error()})
			}

		case "set_config_option":
			if err := session.SetConfigOption(ctx, msg.ConfigID, msg.Value); err != nil {
				stream.publish(chat.ChatEvent{Type: "error", Error: err.Error()})
			}

		case "permission_response":
			session.RespondPermission(chat.PermissionResponse{
				PermissionID: msg.PermissionID,
				OptionID:     msg.OptionID,
				Cancelled:    msg.Cancelled,
			})

		case "set_auto_approve":
			session.SetAutoApprove(msg.AutoApprove)

		default:
			stream.publish(chat.ChatEvent{Type: "error", Error: "unsupported message type: " + msg.Type})
		}
	}
}

func (a *API) handleChatRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	workDir := r.URL.Query().Get("workDir")
	preferredSessionID := r.URL.Query().Get("sessionId")
	if workDir == "" {
		http.Error(w, "workDir parameter is required", http.StatusBadRequest)
		return
	}

	resp := chatRestoreResponse{}

	if a.chatStore == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	var (
		record *chat.SessionRecord
		err    error
	)
	if preferredSessionID != "" {
		record, err = a.chatStore.GetSessionForProject(preferredSessionID, workDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if record == nil {
			writeJSON(w, http.StatusOK, chatRestoreResponse{
				Found:       false,
				SessionID:   preferredSessionID,
				ResumeError: fmt.Sprintf("session %s not found for project", preferredSessionID),
			})
			return
		}
	} else {
		record, err = a.chatStore.GetLastSessionForProject(workDir)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if record == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Found = true
	resp.SessionID = record.ID
	resp.AgentID = record.AgentID
	resp.WorkDir = record.WorkDir
	resp.ACPSessionID = record.ACPSessionID
	resp.Status = record.Status
	resp.Title = record.Title

	if a.chatSessionManager != nil {
		if liveID, ok := a.chatSessionManager.LiveIDForRecordID(record.ID); ok {
			resp.LiveSessionID = liveID
			resp.IsLive = true
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}

	if a.chatSessionManager != nil && a.chatSessionManager.IsLive(record.ID) {
		resp.IsLive = true
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if record.ACPSessionID != "" && record.AgentID != "" {
		agent, ok := findAgentDescriptor(record.AgentID)
		resp.AgentSupportsACP = agent.SupportsACP
		resp.AgentAvailable = agent.Available
		if ok && agent.Available && agent.SupportsACP {
			resp.CanResume = true
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleChatResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if a.chatSessionManager == nil {
		http.Error(w, "chat not available", http.StatusServiceUnavailable)
		return
	}

	var req chatResumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || req.AgentID == "" {
		http.Error(w, "sessionId and agentId are required", http.StatusBadRequest)
		return
	}

	workDir := req.WorkDir
	if workDir == "" {
		workDir = a.workspaceRoot
	}

	agent, ok := findAnyAgentDescriptor(req.AgentID)
	if !ok {
		writeJSON(w, http.StatusOK, chatRestoreResponse{
			Found:       true,
			SessionID:   req.SessionID,
			CanResume:   false,
			ResumeError: "unknown agent: " + req.AgentID,
		})
		return
	}
	if !agent.Available {
		writeJSON(w, http.StatusOK, chatRestoreResponse{
			Found:       true,
			SessionID:   req.SessionID,
			CanResume:   false,
			ResumeError: "agent is not available: " + req.AgentID,
		})
		return
	}

	var session chat.ChatSession
	var err error
	resumed := true
	resumeErr := ""
	if agent.SupportsACP && req.ACPSessionID != "" {
		session, err = a.chatSessionManager.Resume(context.Background(), agent, workDir, req.ACPSessionID)
		if err != nil {
			resumeErr = err.Error()
		}
	} else if agent.SupportsACP {
		resumeErr = "missing ACP session id; creating replacement session"
		err = fmt.Errorf("%s", resumeErr)
	} else {
		resumeErr = "agent does not support ACP session/resume"
		err = fmt.Errorf("%s", resumeErr)
	}
	if err != nil {
		session, err = a.chatSessionManager.Create(context.Background(), agent, workDir)
		if err != nil {
			writeJSON(w, http.StatusOK, chatRestoreResponse{
				Found:       true,
				SessionID:   req.SessionID,
				CanResume:   false,
				ResumeError: resumeErr + "; fallback create failed: " + err.Error(),
			})
			return
		}
		resumed = false
	}

	if a.chatStore != nil {
		if session.ID() != req.SessionID {
			_ = a.chatStore.UpdateSessionACP(req.SessionID, session.ACPSessionID(), workDir)
			a.chatSessionManager.LinkRecordID(session.ID(), req.SessionID)
		} else {
			record, _ := a.chatStore.GetSession(session.ID())
			if record == nil {
				_ = a.chatStore.CreateSessionFull(
					session.ID(),
					session.AgentID(),
					"",
					workDir,
					session.ACPSessionID(),
				)
			} else {
				_ = a.chatStore.UpdateSessionACP(session.ID(), session.ACPSessionID(), workDir)
			}
			a.chatSessionManager.LinkRecordID(session.ID(), session.ID())
		}
	}

	writeJSON(w, http.StatusCreated, chatCreateResponse{
		ID:           session.ID(),
		Mode:         string(session.Mode()),
		IsResumed:    resumed,
		ResumedFrom:  req.SessionID,
		WorkDir:      session.WorkDir(),
		ACPSessionID: session.ACPSessionID(),
	})
}

type chatHistoryMessageRequest struct {
	SessionID    string `json:"sessionId"`
	AgentID      string `json:"agentId"`
	Title        string `json:"title"`
	WorkDir      string `json:"workDir,omitempty"`
	ACPSessionID string `json:"acpSessionId,omitempty"`
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
		workDir := r.URL.Query().Get("workDir")
		var (
			sessions []chat.SessionRecord
			err      error
		)
		if workDir != "" {
			sessions, err = a.chatStore.SessionsForProject(workDir, 50)
		} else {
			sessions, err = a.chatStore.ListSessions(50)
		}
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
			if err := a.chatStore.CreateSessionFull(req.SessionID, agentId, title, req.WorkDir, req.ACPSessionID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			if req.Title != "" {
				_ = a.chatStore.UpdateSessionTitle(req.SessionID, req.Title)
			}
			if req.WorkDir != "" || req.ACPSessionID != "" {
				record, _ := a.chatStore.GetSession(req.SessionID)
				workDir := req.WorkDir
				acpSessionID := req.ACPSessionID
				if record != nil {
					if workDir == "" {
						workDir = record.WorkDir
					}
					if acpSessionID == "" {
						acpSessionID = record.ACPSessionID
					}
				}
				_ = a.chatStore.UpdateSessionACP(req.SessionID, acpSessionID, workDir)
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
		if r.URL.Query().Get("transcript") == "true" {
			events, err := a.chatStore.GetEvents(sessionID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if events == nil {
				events = []chat.EventRecord{}
			}
			snap, _ := a.chatStore.GetSnapshot(sessionID)
			snapshot := snap
			if snapshot == nil {
				snapshot = &chat.SessionSnapshot{SessionID: sessionID}
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"events":   events,
				"snapshot": snapshot,
			})
			return
		}

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

func (a *API) handleChatStateByID(w http.ResponseWriter, r *http.Request) {
	if a.chatStore == nil {
		http.Error(w, "chat history not available", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/chat/state/")
	if sessionID == "" {
		http.NotFound(w, r)
		return
	}

	session, err := a.chatStore.GetSession(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if session == nil {
		http.NotFound(w, r)
		return
	}
	events, err := a.chatStore.GetEvents(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	messages, err := a.chatStore.GetMessages(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	snapshot, err := a.chatStore.GetSnapshot(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session":  session,
		"messages": messages,
		"events":   events,
		"snapshot": snapshot,
	})
}

func (a *API) handleChatInstallACP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AgentID string `json:"agentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	path, err := chat.InstallACPAdapter(req.AgentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func findAgentDescriptor(id string) (chat.AgentDescriptor, bool) {
	agents := discoverAgentDescriptors()
	for _, a := range agents {
		if a.ID == id && a.Available {
			return a, true
		}
	}
	return chat.AgentDescriptor{}, false
}

func findAnyAgentDescriptor(id string) (chat.AgentDescriptor, bool) {
	agents := discoverAgentDescriptors()
	for _, a := range agents {
		if a.ID == id {
			return a, true
		}
	}
	return chat.AgentDescriptor{}, false
}

func formatAttachments(content string, attachments []chatAttachment) string {
	var sb strings.Builder
	for _, att := range attachments {
		if att.Type == "file" {
			data, err := os.ReadFile(att.Path)
			if err == nil {
				sb.WriteString(fmt.Sprintf("\n\nFile: %s\n```\n%s\n```\n", att.Name, string(data)))
			}
		}
	}
	if sb.Len() > 0 {
		return content + sb.String()
	}
	return content
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

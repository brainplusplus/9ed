package httpapi

import (
	"encoding/json"
	"net/http"

	"go-webttyd/internal/chat"
)

type recentProjectRequest struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

func (a *API) handleRecentProjects(w http.ResponseWriter, r *http.Request) {
	if a.chatStore == nil {
		http.Error(w, "store not available", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		projects, err := a.chatStore.ListRecentProjects(20)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if projects == nil {
			projects = []chat.RecentProject{}
		}
		writeJSON(w, http.StatusOK, projects)

	case http.MethodPost:
		var req recentProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Path == "" || req.Name == "" {
			http.Error(w, "path and name are required", http.StatusBadRequest)
			return
		}
		if err := a.chatStore.SaveRecentProject(req.Path, req.Name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case http.MethodDelete:
		var req recentProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Path == "" {
			http.Error(w, "path is required", http.StatusBadRequest)
			return
		}
		if err := a.chatStore.RemoveRecentProject(req.Path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

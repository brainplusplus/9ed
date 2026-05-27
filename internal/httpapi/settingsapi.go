package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/brainplusplus/9ed/internal/appmeta"
	"github.com/brainplusplus/9ed/internal/tunnel"
)

type settingsAboutResponse struct {
	Name                string `json:"name"`
	Version             string `json:"version"`
	Description         string `json:"description"`
	DefaultTunnelEngine string `json:"defaultTunnelEngine"`
	AppPort             string `json:"appPort"`
	AppTunnelEnabled    bool   `json:"appTunnelEnabled"`
}

type settingsTunnelRequest struct {
	Name      string `json:"name"`
	LocalPort string `json:"localPort"`
	Engine    string `json:"engine,omitempty"`
}

func (a *API) handleSettingsAbout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if a.settingsTunnels == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, http.StatusOK, settingsAboutResponse{
		Name:                appmeta.Name,
		Version:             appmeta.Version,
		Description:         appmeta.Description,
		DefaultTunnelEngine: a.settingsTunnels.DefaultEngine(),
		AppPort:             a.settingsTunnels.AppPort(),
		AppTunnelEnabled:    a.settingsTunnels.AppTunnelEnabled(),
	})
}

func (a *API) handleSettingsTunnels(w http.ResponseWriter, r *http.Request) {
	if a.settingsTunnels == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.settingsTunnels.List())
	case http.MethodPost:
		var req settingsTunnelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		record, err := a.settingsTunnels.Save(tunnel.ConfigRecord{
			Name:      req.Name,
			LocalPort: req.LocalPort,
			Engine:    req.Engine,
			Enabled:   false,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, record)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *API) handleSettingsTunnelByID(w http.ResponseWriter, r *http.Request) {
	if a.settingsTunnels == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/settings/tunnels/")
	id, action, _ := strings.Cut(strings.Trim(rest, "/"), "/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodPut && action == "":
		current, ok := a.settingsTunnels.Get(id)
		if !ok {
			http.Error(w, "tunnel not found", http.StatusNotFound)
			return
		}
		var req settingsTunnelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		record, err := a.settingsTunnels.Save(tunnel.ConfigRecord{
			ID:        current.ID,
			Name:      req.Name,
			LocalPort: req.LocalPort,
			Engine:    coalesceString(req.Engine, current.Engine),
			Enabled:   current.Enabled,
			CreatedAt: current.CreatedAt,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, record)
	case r.Method == http.MethodDelete && action == "":
		if err := a.settingsTunnels.Delete(id); err != nil {
			if err.Error() == "tunnel not found" {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && action == "start":
		record, err := a.settingsTunnels.Start(id)
		if err != nil {
			if err.Error() == "tunnel not found" {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, record)
	case r.Method == http.MethodPost && action == "stop":
		record, err := a.settingsTunnels.Stop(id)
		if err != nil {
			if err.Error() == "tunnel not found" {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, record)
	case r.Method == http.MethodPost && action == "restart":
		record, err := a.settingsTunnels.Restart(id)
		if err != nil {
			if err.Error() == "tunnel not found" {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, record)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func coalesceString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

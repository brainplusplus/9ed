package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brainplusplus/9ed/internal/tunnel"
)

type settingsTestStore struct {
	configs map[string]tunnel.ConfigRecord
}

func (s *settingsTestStore) ListTunnelConfigs() ([]tunnel.ConfigRecord, error) {
	items := make([]tunnel.ConfigRecord, 0, len(s.configs))
	for _, cfg := range s.configs {
		items = append(items, cfg)
	}
	return items, nil
}

func (s *settingsTestStore) SaveTunnelConfig(cfg tunnel.ConfigRecord) error {
	if s.configs == nil {
		s.configs = make(map[string]tunnel.ConfigRecord)
	}
	s.configs[cfg.ID] = cfg
	return nil
}

func (s *settingsTestStore) DeleteTunnelConfig(id string) error {
	delete(s.configs, id)
	return nil
}

type settingsRuntime struct {
	url string
}

func (r *settingsRuntime) URL() string { return r.url }
func (r *settingsRuntime) Stop() error { return nil }

func TestSettingsAboutRoute(t *testing.T) {
	manager := tunnel.NewManagerWithStarter(&settingsTestStore{}, "8183", true, "bore", func(engine, port string) (tunnel.RuntimeTunnel, error) {
		return &settingsRuntime{url: "http://bore.pub:1234"}, nil
	})
	api := New(Dependencies{SettingsTunnels: manager})

	req := httptest.NewRequest(http.MethodGet, "/api/settings/about", nil)
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["defaultTunnelEngine"] != "bore" {
		t.Fatalf("expected default engine bore, got %#v", payload["defaultTunnelEngine"])
	}
}

func TestSettingsTunnelRoutes(t *testing.T) {
	store := &settingsTestStore{}
	manager := tunnel.NewManagerWithStarter(store, "8183", false, "cloudflare", func(engine, port string) (tunnel.RuntimeTunnel, error) {
		return &settingsRuntime{url: "https://preview.trycloudflare.com"}, nil
	})
	api := New(Dependencies{SettingsTunnels: manager})

	body := []byte(`{"name":"Preview","localPort":"3000","engine":"cloudflare"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/settings/tunnels", bytes.NewReader(body))
	createRec := httptest.NewRecorder()
	api.Handler().ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}

	var created tunnel.RuntimeRecord
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created: %v", err)
	}

	startReq := httptest.NewRequest(http.MethodPost, "/api/settings/tunnels/"+created.ID+"/start", nil)
	startRec := httptest.NewRecorder()
	api.Handler().ServeHTTP(startRec, startReq)

	if startRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from start, got %d: %s", startRec.Code, startRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/settings/tunnels", nil)
	listRec := httptest.NewRecorder()
	api.Handler().ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from list, got %d: %s", listRec.Code, listRec.Body.String())
	}

	var tunnels []tunnel.RuntimeRecord
	if err := json.Unmarshal(listRec.Body.Bytes(), &tunnels); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(tunnels) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(tunnels))
	}
}

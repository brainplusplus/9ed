package tunnel

import (
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	configs map[string]ConfigRecord
}

func (s *fakeStore) ListTunnelConfigs() ([]ConfigRecord, error) {
	items := make([]ConfigRecord, 0, len(s.configs))
	for _, cfg := range s.configs {
		items = append(items, cfg)
	}
	return items, nil
}

func (s *fakeStore) SaveTunnelConfig(cfg ConfigRecord) error {
	if s.configs == nil {
		s.configs = make(map[string]ConfigRecord)
	}
	s.configs[cfg.ID] = cfg
	return nil
}

func (s *fakeStore) DeleteTunnelConfig(id string) error {
	delete(s.configs, id)
	return nil
}

type fakeRuntime struct {
	url     string
	stopped bool
}

func (r *fakeRuntime) URL() string { return r.url }
func (r *fakeRuntime) Stop() error { r.stopped = true; return nil }

func TestManagerRejectsBuiltInTunnelPortConflict(t *testing.T) {
	manager := NewManagerWithStarter(&fakeStore{}, "8080", true, "cloudflare", func(engine, port string) (RuntimeTunnel, error) {
		return &fakeRuntime{url: "https://example.trycloudflare.com"}, nil
	})

	_, err := manager.Save(ConfigRecord{
		Name:      "Conflict",
		LocalPort: "8080",
	})
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestManagerStartAndStopPersistEnabledState(t *testing.T) {
	store := &fakeStore{}
	startCalls := 0
	manager := NewManagerWithStarter(store, "8080", false, "cloudflare", func(engine, port string) (RuntimeTunnel, error) {
		startCalls++
		return &fakeRuntime{url: "https://running.trycloudflare.com"}, nil
	})

	record, err := manager.Save(ConfigRecord{
		Name:      "Preview",
		LocalPort: "3000",
	})
	if err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if record.Enabled {
		t.Fatal("expected tunnel to start disabled")
	}

	record, err = manager.Start(record.ID)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if !record.Enabled {
		t.Fatal("expected enabled state after start")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, ok := manager.Get(record.ID)
		if ok && current.Status == StatusStarted {
			if current.URL == "" {
				t.Fatal("expected running tunnel URL")
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	stopped, err := manager.Stop(record.ID)
	if err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if stopped.Enabled {
		t.Fatal("expected tunnel to be disabled after stop")
	}
	if store.configs[record.ID].Enabled {
		t.Fatal("expected disabled state to be persisted")
	}
	if startCalls == 0 {
		t.Fatal("expected starter to be called")
	}
}

func TestManagerLoadsEnabledTunnels(t *testing.T) {
	store := &fakeStore{
		configs: map[string]ConfigRecord{
			"tunnel-1": {
				ID:        "tunnel-1",
				Name:      "Auto Start",
				LocalPort: "4321",
				Engine:    "cloudflare",
				Enabled:   true,
				CreatedAt: time.Now().UnixMilli(),
				UpdatedAt: time.Now().UnixMilli(),
			},
		},
	}
	manager := NewManagerWithStarter(store, "8080", false, "cloudflare", func(engine, port string) (RuntimeTunnel, error) {
		if port != "4321" {
			return nil, errors.New("unexpected port")
		}
		return &fakeRuntime{url: "https://auto.trycloudflare.com"}, nil
	})

	if err := manager.Load(); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		record, ok := manager.Get("tunnel-1")
		if ok && record.Status == StatusStarted && record.URL != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected enabled tunnel to auto-start")
}

func TestManagerRetriesEnabledTunnelUntilStartSucceeds(t *testing.T) {
	store := &fakeStore{}
	startCalls := 0
	manager := NewManagerWithStarter(store, "8080", false, "cloudflare", func(engine, port string) (RuntimeTunnel, error) {
		startCalls++
		if startCalls < 3 {
			return nil, errors.New("temporary tunnel failure")
		}
		return &fakeRuntime{url: "https://retry.trycloudflare.com"}, nil
	})
	manager.retryBackoff = []time.Duration{5 * time.Millisecond}

	record, err := manager.Save(ConfigRecord{
		Name:      "Retrying Preview",
		LocalPort: "5173",
	})
	if err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if _, err := manager.Start(record.ID); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, ok := manager.Get(record.ID)
		if ok && current.Status == StatusStarted && current.URL == "https://retry.trycloudflare.com" {
			if startCalls != 3 {
				t.Fatalf("expected 3 start attempts, got %d", startCalls)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("expected tunnel to retry until started; attempts=%d", startCalls)
}

func TestManagerStopCancelsPendingRetry(t *testing.T) {
	store := &fakeStore{}
	started := make(chan struct{}, 1)
	startCalls := 0
	manager := NewManagerWithStarter(store, "8080", false, "cloudflare", func(engine, port string) (RuntimeTunnel, error) {
		startCalls++
		started <- struct{}{}
		return nil, errors.New("temporary tunnel failure")
	})
	manager.retryBackoff = []time.Duration{25 * time.Millisecond}

	record, err := manager.Save(ConfigRecord{
		Name:      "Stop Retry",
		LocalPort: "3333",
	})
	if err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if _, err := manager.Start(record.ID); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected first start attempt")
	}

	if _, err := manager.Stop(record.ID); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	time.Sleep(75 * time.Millisecond)

	if startCalls != 1 {
		t.Fatalf("expected stop to cancel retry loop, got %d start attempts", startCalls)
	}
	current, ok := manager.Get(record.ID)
	if !ok {
		t.Fatal("expected tunnel record")
	}
	if current.Status != StatusStopped {
		t.Fatalf("expected stopped status, got %q", current.Status)
	}
}

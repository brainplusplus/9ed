package tunnel

import (
	"errors"
	"fmt"
	"log"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	StatusStarting = "starting"
	StatusStarted  = "started"
	StatusStopped  = "stopped"
)

var settingsTunnelRetryBackoff = []time.Duration{
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

type ConfigRecord struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	LocalPort string `json:"localPort"`
	Engine    string `json:"engine"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type RuntimeRecord struct {
	ConfigRecord
	Status    string `json:"status"`
	URL       string `json:"url,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

type Store interface {
	ListTunnelConfigs() ([]ConfigRecord, error)
	SaveTunnelConfig(ConfigRecord) error
	DeleteTunnelConfig(id string) error
}

type RuntimeTunnel interface {
	URL() string
	Stop() error
}

type Starter func(engine, port string) (RuntimeTunnel, error)

type managedTunnel struct {
	config    ConfigRecord
	runtime   RuntimeTunnel
	status    string
	lastError string
	launchSeq uint64
}

type Manager struct {
	mu               sync.RWMutex
	store            Store
	startTunnel      Starter
	appPort          string
	appTunnelEnabled bool
	defaultEngine    string
	tunnels          map[string]*managedTunnel
	retryBackoff     []time.Duration
}

func NewManager(store Store, appPort string, appTunnelEnabled bool, defaultEngine string) *Manager {
	return NewManagerWithStarter(store, appPort, appTunnelEnabled, defaultEngine, func(engine, port string) (RuntimeTunnel, error) {
		return Start(engine, port)
	})
}

func NewManagerWithStarter(store Store, appPort string, appTunnelEnabled bool, defaultEngine string, starter Starter) *Manager {
	engine := strings.TrimSpace(strings.ToLower(defaultEngine))
	if engine == "" {
		engine = "cloudflare"
	}
	return &Manager{
		store:            store,
		startTunnel:      starter,
		appPort:          strings.TrimSpace(appPort),
		appTunnelEnabled: appTunnelEnabled,
		defaultEngine:    engine,
		tunnels:          make(map[string]*managedTunnel),
		retryBackoff:     settingsTunnelRetryBackoff,
	}
}

func (m *Manager) Load() error {
	if m.store == nil {
		return nil
	}

	configs, err := m.store.ListTunnelConfigs()
	if err != nil {
		return err
	}

	m.mu.Lock()
	existingPorts := make(map[string]string, len(configs))
	for _, cfg := range configs {
		normalized, normalizeErr := m.normalizeConfigWithPorts(cfg, existingPorts)
		if normalizeErr != nil {
			m.tunnels[cfg.ID] = &managedTunnel{
				config:    cfg,
				status:    StatusStopped,
				lastError: normalizeErr.Error(),
			}
			continue
		}
		m.tunnels[normalized.ID] = &managedTunnel{
			config: normalized,
			status: StatusStopped,
		}
		existingPorts[normalized.LocalPort] = normalized.ID
	}
	idsToStart := make([]string, 0, len(m.tunnels))
	for id, tunnel := range m.tunnels {
		if tunnel.config.Enabled {
			idsToStart = append(idsToStart, id)
		}
	}
	m.mu.Unlock()

	for _, id := range idsToStart {
		m.startAsync(id)
	}

	return nil
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	runtimes := make([]RuntimeTunnel, 0, len(m.tunnels))
	for _, tunnel := range m.tunnels {
		tunnel.launchSeq++
		tunnel.status = StatusStopped
		if tunnel.runtime != nil {
			runtimes = append(runtimes, tunnel.runtime)
			tunnel.runtime = nil
		}
	}
	m.mu.Unlock()

	for _, runtime := range runtimes {
		_ = runtime.Stop()
	}
}

func (m *Manager) List() []RuntimeRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]RuntimeRecord, 0, len(m.tunnels))
	for _, tunnel := range m.tunnels {
		item := RuntimeRecord{
			ConfigRecord: tunnel.config,
			Status:       tunnel.status,
			LastError:    tunnel.lastError,
		}
		if tunnel.runtime != nil {
			item.URL = tunnel.runtime.URL()
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		}
		return items[i].CreatedAt < items[j].CreatedAt
	})

	return items
}

func (m *Manager) Get(id string) (RuntimeRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tunnel, ok := m.tunnels[id]
	if !ok {
		return RuntimeRecord{}, false
	}

	item := RuntimeRecord{
		ConfigRecord: tunnel.config,
		Status:       tunnel.status,
		LastError:    tunnel.lastError,
	}
	if tunnel.runtime != nil {
		item.URL = tunnel.runtime.URL()
	}
	return item, true
}

func (m *Manager) recordByID(id string) (RuntimeRecord, error) {
	record, ok := m.Get(id)
	if !ok {
		return RuntimeRecord{}, errors.New("tunnel not found")
	}
	return record, nil
}

func (m *Manager) DefaultEngine() string {
	return m.defaultEngine
}

func (m *Manager) AppPort() string {
	return m.appPort
}

func (m *Manager) AppTunnelEnabled() bool {
	return m.appTunnelEnabled
}

func (m *Manager) Save(cfg ConfigRecord) (RuntimeRecord, error) {
	m.mu.Lock()
	existing, exists := m.tunnels[cfg.ID]
	existingPorts := m.snapshotPortsLocked(cfg.ID)
	if exists {
		cfg.CreatedAt = existing.config.CreatedAt
		cfg.Enabled = existing.config.Enabled
		if cfg.Engine == "" {
			cfg.Engine = existing.config.Engine
		}
	}
	normalized, err := m.normalizeConfigWithPorts(cfg, existingPorts)
	if err != nil {
		m.mu.Unlock()
		return RuntimeRecord{}, err
	}

	var runtimeToStop RuntimeTunnel
	if exists && existing.runtime != nil && (existing.config.LocalPort != normalized.LocalPort || existing.config.Engine != normalized.Engine) {
		runtimeToStop = existing.runtime
		existing.runtime = nil
	}

	if exists {
		existing.launchSeq++
		existing.config = normalized
		existing.lastError = ""
		if runtimeToStop != nil || !normalized.Enabled {
			existing.status = StatusStopped
		}
	} else {
		m.tunnels[normalized.ID] = &managedTunnel{
			config: normalized,
			status: StatusStopped,
		}
	}
	m.mu.Unlock()

	if m.store != nil {
		if err := m.store.SaveTunnelConfig(normalized); err != nil {
			return RuntimeRecord{}, err
		}
	}

	if runtimeToStop != nil {
		_ = runtimeToStop.Stop()
	}
	if normalized.Enabled {
		m.startAsync(normalized.ID)
	}
	return m.recordByID(normalized.ID)
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	tunnel, ok := m.tunnels[id]
	if !ok {
		m.mu.Unlock()
		return errors.New("tunnel not found")
	}
	tunnel.launchSeq++
	runtime := tunnel.runtime
	delete(m.tunnels, id)
	m.mu.Unlock()

	if m.store != nil {
		if err := m.store.DeleteTunnelConfig(id); err != nil {
			return err
		}
	}
	if runtime != nil {
		_ = runtime.Stop()
	}
	return nil
}

func (m *Manager) Start(id string) (RuntimeRecord, error) {
	m.mu.Lock()
	tunnel, ok := m.tunnels[id]
	if !ok {
		m.mu.Unlock()
		return RuntimeRecord{}, errors.New("tunnel not found")
	}
	tunnel.config.Enabled = true
	tunnel.config.UpdatedAt = time.Now().UnixMilli()
	cfg := tunnel.config
	m.mu.Unlock()

	if m.store != nil {
		if err := m.store.SaveTunnelConfig(cfg); err != nil {
			return RuntimeRecord{}, err
		}
	}

	m.startAsync(id)
	return m.recordByID(id)
}

func (m *Manager) Stop(id string) (RuntimeRecord, error) {
	m.mu.Lock()
	tunnel, ok := m.tunnels[id]
	if !ok {
		m.mu.Unlock()
		return RuntimeRecord{}, errors.New("tunnel not found")
	}
	tunnel.launchSeq++
	tunnel.config.Enabled = false
	tunnel.config.UpdatedAt = time.Now().UnixMilli()
	runtime := tunnel.runtime
	tunnel.runtime = nil
	tunnel.status = StatusStopped
	tunnel.lastError = ""
	cfg := tunnel.config
	m.mu.Unlock()

	if m.store != nil {
		if err := m.store.SaveTunnelConfig(cfg); err != nil {
			return RuntimeRecord{}, err
		}
	}
	if runtime != nil {
		_ = runtime.Stop()
	}
	return m.recordByID(id)
}

func (m *Manager) Restart(id string) (RuntimeRecord, error) {
	m.mu.Lock()
	tunnel, ok := m.tunnels[id]
	if !ok {
		m.mu.Unlock()
		return RuntimeRecord{}, errors.New("tunnel not found")
	}
	tunnel.config.Enabled = true
	tunnel.config.UpdatedAt = time.Now().UnixMilli()
	runtime := tunnel.runtime
	tunnel.runtime = nil
	tunnel.lastError = ""
	cfg := tunnel.config
	m.mu.Unlock()

	if m.store != nil {
		if err := m.store.SaveTunnelConfig(cfg); err != nil {
			return RuntimeRecord{}, err
		}
	}
	if runtime != nil {
		_ = runtime.Stop()
	}
	m.startAsync(id)
	return m.recordByID(id)
}

func (m *Manager) startAsync(id string) {
	m.mu.Lock()
	tunnel, ok := m.tunnels[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	if !tunnel.config.Enabled {
		tunnel.status = StatusStopped
		m.mu.Unlock()
		return
	}

	tunnel.launchSeq++
	seq := tunnel.launchSeq
	cfg := tunnel.config
	runtimeToStop := tunnel.runtime
	tunnel.runtime = nil
	tunnel.status = StatusStarting
	tunnel.lastError = ""
	m.mu.Unlock()

	if runtimeToStop != nil {
		_ = runtimeToStop.Stop()
	}

	go m.startLoop(id, seq, cfg)
}

func (m *Manager) startLoop(id string, seq uint64, cfg ConfigRecord) {
	attempt := 0
	for {
		runtime, err := m.startTunnel(cfg.Engine, cfg.LocalPort)
		if err == nil && runtime == nil {
			err = errors.New("tunnel starter returned nil runtime")
		}

		m.mu.Lock()
		current, ok := m.tunnels[id]
		if !ok || current.launchSeq != seq || !current.config.Enabled {
			m.mu.Unlock()
			if runtime != nil {
				_ = runtime.Stop()
			}
			return
		}
		if err == nil {
			current.runtime = runtime
			current.status = StatusStarted
			current.lastError = ""
			m.mu.Unlock()
			return
		}

		current.runtime = nil
		current.status = StatusStarting
		current.lastError = err.Error()
		name := current.config.Name
		port := current.config.LocalPort
		delay := retryDelay(m.retryBackoff, attempt)
		attempt++
		m.mu.Unlock()

		if runtime != nil {
			_ = runtime.Stop()
		}
		log.Printf("tunnel: failed to start %s on port %s: %v; retrying in %s", name, port, err, delay)
		if !m.waitForRetry(id, seq, delay) {
			return
		}
	}
}

func (m *Manager) waitForRetry(id string, seq uint64, delay time.Duration) bool {
	if delay <= 0 {
		runtime.Gosched()
		return m.shouldContinueStart(id, seq)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	<-timer.C
	return m.shouldContinueStart(id, seq)
}

func (m *Manager) shouldContinueStart(id string, seq uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	current, ok := m.tunnels[id]
	return ok && current.launchSeq == seq && current.config.Enabled
}

func retryDelay(backoff []time.Duration, attempt int) time.Duration {
	if len(backoff) == 0 {
		return time.Second
	}
	if attempt < len(backoff) {
		return backoff[attempt]
	}
	return backoff[len(backoff)-1]
}

func (m *Manager) snapshotPortsLocked(excludeID string) map[string]string {
	ports := make(map[string]string, len(m.tunnels))
	for id, tunnel := range m.tunnels {
		if id == excludeID {
			continue
		}
		ports[tunnel.config.LocalPort] = id
	}
	return ports
}

func (m *Manager) normalizeConfigWithPorts(cfg ConfigRecord, existingPorts map[string]string) (ConfigRecord, error) {
	normalized := cfg
	normalized.ID = strings.TrimSpace(normalized.ID)
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.LocalPort = strings.TrimSpace(normalized.LocalPort)
	normalized.Engine = strings.TrimSpace(strings.ToLower(normalized.Engine))

	if normalized.ID == "" {
		normalized.ID = fmt.Sprintf("tunnel-%d", time.Now().UnixNano())
	}
	if normalized.Name == "" {
		return ConfigRecord{}, errors.New("name is required")
	}
	if normalized.LocalPort == "" {
		return ConfigRecord{}, errors.New("local port is required")
	}
	port, err := strconv.Atoi(normalized.LocalPort)
	if err != nil || port < 1 || port > 65535 {
		return ConfigRecord{}, errors.New("local port must be between 1 and 65535")
	}
	if m.appTunnelEnabled && normalized.LocalPort == m.appPort {
		return ConfigRecord{}, errors.New("local port conflicts with 9ed built-in tunnel")
	}
	if _, exists := existingPorts[normalized.LocalPort]; exists {
		return ConfigRecord{}, errors.New("local port is already used by another tunnel")
	}

	if normalized.Engine == "" {
		normalized.Engine = m.defaultEngine
	}
	if normalized.Engine != "bore" && normalized.Engine != "cloudflare" {
		return ConfigRecord{}, errors.New("engine must be either bore or cloudflare")
	}

	now := time.Now().UnixMilli()
	if normalized.CreatedAt == 0 {
		normalized.CreatedAt = now
	}
	normalized.UpdatedAt = now

	return normalized, nil
}

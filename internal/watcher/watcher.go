package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/brainplusplus/9ed/internal/debug"
)

type Event struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Name string `json:"name"`
}

type Subscriber struct {
	Ch     chan Event
	Root   string
	closed bool
	mu     sync.Mutex
}

func (s *Subscriber) Send(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		debug.Printf("[watcher] Send skipped: subscriber closed, event=%s name=%s", e.Type, e.Name)
		return
	}
	select {
	case s.Ch <- e:
	default:
		debug.Printf("[watcher] Send DROPPED: channel full (len=%d), event=%s name=%s", len(s.Ch), e.Type, e.Name)
	}
}

func (s *Subscriber) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.Ch)
	}
}

type FileWatcher struct {
	fsw         *fsnotify.Watcher
	mu          sync.RWMutex
	subscribers map[*Subscriber]struct{}
	debounce    map[string]*time.Timer
	debounceMu  sync.Mutex
}

func New() (*FileWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	fw := &FileWatcher{
		fsw:         fsw,
		subscribers: make(map[*Subscriber]struct{}),
		debounce:    make(map[string]*time.Timer),
	}

	go fw.loop()

	return fw, nil
}

func (fw *FileWatcher) Subscribe(root string) *Subscriber {
	sub := &Subscriber{
		Ch:   make(chan Event, 64),
		Root: root,
	}

	fw.mu.Lock()
	fw.subscribers[sub] = struct{}{}
	fw.mu.Unlock()

	return sub
}

func (fw *FileWatcher) Unsubscribe(sub *Subscriber) {
	fw.mu.Lock()
	delete(fw.subscribers, sub)
	fw.mu.Unlock()
	sub.Close()
}

func (fw *FileWatcher) WatchRecursive(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if shouldSkipDir(info.Name()) {
				return filepath.SkipDir
			}
			return fw.fsw.Add(path)
		}
		return nil
	})
}

func (fw *FileWatcher) Close() error {
	fw.mu.Lock()
	for sub := range fw.subscribers {
		sub.Close()
	}
	fw.subscribers = make(map[*Subscriber]struct{})
	fw.mu.Unlock()

	return fw.fsw.Close()
}

func (fw *FileWatcher) loop() {
	for {
		select {
		case fsEvent, ok := <-fw.fsw.Events:
			if !ok {
				return
			}
			fw.handleEvent(fsEvent)

		case _, ok := <-fw.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (fw *FileWatcher) handleEvent(fsEvent fsnotify.Event) {
	eventType := classifyEvent(fsEvent.Op)
	if eventType == "" {
		return
	}

	path := fsEvent.Name

	if shouldSkipEventPath(path) {
		return
	}

	debug.Printf("[watcher] fsnotify event: op=%s type=%s path=%s", fsEvent.Op, eventType, path)

	if fsEvent.Op.Has(fsnotify.Create) {
		if info, err := os.Stat(path); err == nil && info.IsDir() && !shouldSkipDir(info.Name()) {
			_ = fw.fsw.Add(path)
			debug.Printf("[watcher] Auto-watching new dir: %s", path)
		}
	}

	fw.debounceMu.Lock()
	if timer, exists := fw.debounce[path]; exists {
		timer.Stop()
	}
	fw.debounce[path] = time.AfterFunc(100*time.Millisecond, func() {
		fw.debounceMu.Lock()
		delete(fw.debounce, path)
		fw.debounceMu.Unlock()

		event := Event{
			Type: eventType,
			Path: filepath.ToSlash(path),
			Name: filepath.Base(path),
		}

		fw.mu.RLock()
		subCount := len(fw.subscribers)
		sent := 0
		for sub := range fw.subscribers {
			normalizedPath := filepath.ToSlash(path)
			normalizedRoot := filepath.ToSlash(sub.Root)
			if strings.HasPrefix(normalizedPath, normalizedRoot) {
				sub.Send(event)
				sent++
			}
		}
		fw.mu.RUnlock()
		debug.Printf("[watcher] Dispatched event: type=%s name=%s subscribers=%d sent=%d", eventType, filepath.Base(path), subCount, sent)
	})
	fw.debounceMu.Unlock()
}

func classifyEvent(op fsnotify.Op) string {
	switch {
	case op.Has(fsnotify.Create):
		return "create"
	case op.Has(fsnotify.Write):
		return "modify"
	case op.Has(fsnotify.Remove):
		return "delete"
	case op.Has(fsnotify.Rename):
		return "rename"
	default:
		return ""
	}
}

func shouldSkipDir(name string) bool {
	skip := map[string]bool{
		".git": true, "node_modules": true, ".next": true,
		"__pycache__": true, ".cache": true, "dist": true,
		".idea": true, ".vscode": true,
	}
	return skip[name]
}

func shouldSkipEventPath(path string) bool {
	normalized := filepath.ToSlash(path)
	for _, seg := range strings.Split(normalized, "/") {
		if shouldSkipDir(seg) {
			return true
		}
	}
	return false
}

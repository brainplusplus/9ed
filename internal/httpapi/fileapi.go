package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/brainplusplus/9ed/internal/debug"
	"github.com/brainplusplus/9ed/internal/filesystem"
	"github.com/brainplusplus/9ed/internal/git"
)

const maxFileSize = 10 * 1024 * 1024

func (a *API) validatePath(w http.ResponseWriter, raw string) (string, bool) {
	validated, err := filesystem.ValidatePath(a.workspaceRoot, raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return "", false
	}
	return validated, true
}

func (a *API) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	resp := configResponse{
		WorkspaceRoot:      a.workspaceRoot,
		TerminalAIMaxLines: a.terminalAiMaxLines,
	}
	if a.tunnelURL != nil {
		resp.TunnelURL = a.tunnelURL()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleFileDrives(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	drives := filesystem.ListDrives()
	if volume := filepath.VolumeName(a.workspaceRoot); volume != "" {
		rootDrive := volume + string(filepath.Separator)
		found := false
		for _, drive := range drives {
			if drive == rootDrive {
				found = true
				break
			}
		}
		if !found {
			drives = append([]string{rootDrive}, drives...)
		}
	}
	writeJSON(w, http.StatusOK, drives)
}

type fileTreeEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Ignored  bool   `json:"ignored,omitempty"`
}

func (a *API) handleFileTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	dirPath := r.URL.Query().Get("path")
	if dirPath == "" {
		if a.workspaceRoot != "" {
			dirPath = a.workspaceRoot
		} else {
			dirPath = "/"
		}
	}

	validated, ok := a.validatePath(w, dirPath)
	if !ok {
		return
	}

	entries, err := filesystem.ListDirectory(validated)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	repo := git.New(validated)
	ignoredMap := make(map[string]bool)
	if repo.IsRepo() {
		paths := make([]string, len(entries))
		for i, e := range entries {
			paths[i] = e.Name
		}
		ignored, igErr := repo.CheckIgnored(paths)
		if igErr == nil {
			for i, e := range entries {
				if ignored[i] {
					ignoredMap[e.Name] = true
				}
			}
		}
	}

	result := make([]fileTreeEntry, len(entries))
	for i, e := range entries {
		result[i] = fileTreeEntry{
			Name:     e.Name,
			Type:     e.Type,
			Size:     e.Size,
			Modified: e.Modified,
			Ignored:  ignoredMap[e.Name],
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *API) handleFileContent(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	validated, ok := a.validatePath(w, filePath)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		content, err := filesystem.ReadFile(validated, maxFileSize)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "file not found", http.StatusNotFound)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		// ETag mirrors content.Version so HTTP-aware clients (or proxies) can
		// use If-Match without parsing the JSON body.
		if content.Version != "" {
			w.Header().Set("ETag", `"`+content.Version+`"`)
		}
		writeJSON(w, http.StatusOK, content)

	case http.MethodPut:
		var req writeFileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Allow the precondition to be expressed either via the JSON body
		// (req.IfMatch) or the standard HTTP If-Match header. Body wins
		// when both are provided.
		ifMatch := req.IfMatch
		if ifMatch == "" {
			ifMatch = strings.Trim(r.Header.Get("If-Match"), `"`)
		}

		newVersion, err := filesystem.WriteFileAtomic(validated, req.Content, ifMatch)
		if err != nil {
			if errors.Is(err, filesystem.ErrVersionMismatch) {
				http.Error(w, "file was modified by another client; reload before saving", http.StatusPreconditionFailed)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if newVersion != "" {
			w.Header().Set("ETag", `"`+newVersion+`"`)
		}
		writeJSON(w, http.StatusOK, writeFileResponse{Version: newVersion})

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *API) handleFileCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req createEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	validated, ok := a.validatePath(w, req.Path)
	if !ok {
		return
	}

	if err := filesystem.CreateEntry(validated, req.Type); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) handleFileRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req renameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	oldValidated, ok := a.validatePath(w, req.OldPath)
	if !ok {
		return
	}
	newValidated, ok := a.validatePath(w, req.NewPath)
	if !ok {
		return
	}

	if err := filesystem.RenameEntry(oldValidated, newValidated); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (a *API) handleFileCopy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req copyMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	srcValidated, ok := a.validatePath(w, req.SourcePath)
	if !ok {
		return
	}
	dstValidated, ok := a.validatePath(w, req.DestPath)
	if !ok {
		return
	}

	if err := filesystem.CopyEntry(srcValidated, dstValidated); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) handleFileMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req copyMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	srcValidated, ok := a.validatePath(w, req.SourcePath)
	if !ok {
		return
	}
	dstValidated, ok := a.validatePath(w, req.DestPath)
	if !ok {
		return
	}

	if err := filesystem.RenameEntry(srcValidated, dstValidated); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (a *API) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	validated, ok := a.validatePath(w, filePath)
	if !ok {
		return
	}

	if err := filesystem.DeleteEntry(validated); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleFileSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	root := r.URL.Query().Get("root")
	query := r.URL.Query().Get("query")
	regexStr := r.URL.Query().Get("regex")
	maxStr := r.URL.Query().Get("maxResults")

	if root == "" || query == "" {
		http.Error(w, "root and query parameters are required", http.StatusBadRequest)
		return
	}

	validated, ok := a.validatePath(w, root)
	if !ok {
		return
	}

	useRegex := regexStr == "true"
	maxResults := 100
	if maxStr != "" {
		if parsed, err := strconv.Atoi(maxStr); err == nil && parsed > 0 {
			maxResults = parsed
		}
	}

	results, err := filesystem.Search(validated, query, useRegex, maxResults)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, results)
}

func (a *API) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	validated, ok := a.validatePath(w, filePath)
	if !ok {
		return
	}

	info, err := os.Stat(validated)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot download a directory", http.StatusBadRequest)
		return
	}

	fileName := filepath.Base(validated)
	contentType := mime.TypeByExtension(filepath.Ext(fileName))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))

	file, err := os.Open(validated)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	io.Copy(w, file)
}

func (a *API) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	targetDir := r.URL.Query().Get("path")
	if targetDir == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	validated, ok := a.validatePath(w, targetDir)
	if !ok {
		return
	}

	if err := r.ParseMultipartForm(maxFileSize); err != nil {
		http.Error(w, "failed to parse upload", http.StatusBadRequest)
		return
	}

	for _, fileHeaders := range r.MultipartForm.File {
		for _, fh := range fileHeaders {
			src, err := fh.Open()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			destPath := filepath.Join(validated, fh.Filename)
			dst, err := os.Create(destPath)
			if err != nil {
				src.Close()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			_, err = io.Copy(dst, src)
			src.Close()
			dst.Close()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) handleFileWatch(w http.ResponseWriter, r *http.Request) {
	if a.watcher == nil {
		debug.Printf("[ws/watch] Rejected: watcher is nil")
		http.Error(w, "file watcher not available", http.StatusServiceUnavailable)
		return
	}

	root := r.URL.Query().Get("root")
	if root == "" {
		debug.Printf("[ws/watch] Rejected: missing root param")
		http.Error(w, "root parameter is required", http.StatusBadRequest)
		return
	}

	validated, ok := a.validatePath(w, root)
	if !ok {
		debug.Printf("[ws/watch] Rejected: invalid path %q", root)
		return
	}

	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws/watch] WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	debug.Printf("[ws/watch] Client connected, root=%s validated=%s", root, validated)

	if err := a.watcher.WatchRecursive(validated); err != nil {
		log.Printf("[ws/watch] WatchRecursive failed: %v", err)
		_ = conn.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		return
	}

	sub := a.watcher.Subscribe(validated)
	defer a.watcher.Unsubscribe(sub)
	debug.Printf("[ws/watch] Subscribed to watcher, root=%s", validated)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				debug.Printf("[ws/watch] Client read loop ended: %v", err)
				return
			}
		}
	}()

	eventCount := 0
	for {
		select {
		case <-done:
			debug.Printf("[ws/watch] Channel closed, sent %d total events", eventCount)
			return
		case event, ok := <-sub.Ch:
			if !ok {
				debug.Printf("[ws/watch] Channel closed, sent %d total events", eventCount)
				return
			}
			eventCount++
			if err := conn.WriteJSON(event); err != nil {
				debug.Printf("[ws/watch] WriteJSON failed after %d events: %v", eventCount, err)
				return
			}
			if eventCount <= 5 || eventCount%50 == 0 {
				debug.Printf("[ws/watch] Sent event #%d: type=%s name=%s path=%s", eventCount, event.Type, event.Name, event.Path)
			}
		}
	}
}

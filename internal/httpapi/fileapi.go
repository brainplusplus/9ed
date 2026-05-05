package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"go-webttyd/internal/filesystem"
	"go-webttyd/internal/git"
)

const maxFileSize = 10 * 1024 * 1024

func (a *API) requireFullMode(w http.ResponseWriter) bool {
	if a.mode != "full" {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return false
	}
	return true
}

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
	writeJSON(w, http.StatusOK, configResponse{
		Mode:          a.mode,
		WorkspaceRoot: a.workspaceRoot,
	})
}

func (a *API) handleFileDrives(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, filesystem.ListDrives())
}

type fileTreeEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Ignored  bool   `json:"ignored,omitempty"`
}

func (a *API) handleFileTree(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
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
	if !a.requireFullMode(w) {
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
		writeJSON(w, http.StatusOK, content)

	case http.MethodPut:
		var req writeFileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := filesystem.WriteFile(validated, req.Content); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *API) handleFileCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
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
	if !a.requireFullMode(w) {
		return
	}
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
	if !a.requireFullMode(w) {
		return
	}
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
	if !a.requireFullMode(w) {
		return
	}
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
	if !a.requireFullMode(w) {
		return
	}
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
	if !a.requireFullMode(w) {
		return
	}
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
	if !a.requireFullMode(w) {
		return
	}
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
	if !a.requireFullMode(w) {
		return
	}
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
	if !a.requireFullMode(w) {
		return
	}
	if a.watcher == nil {
		http.Error(w, "file watcher not available", http.StatusServiceUnavailable)
		return
	}

	root := r.URL.Query().Get("root")
	if root == "" {
		http.Error(w, "root parameter is required", http.StatusBadRequest)
		return
	}

	validated, ok := a.validatePath(w, root)
	if !ok {
		return
	}

	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	if err := a.watcher.WatchRecursive(validated); err != nil {
		_ = conn.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		return
	}

	sub := a.watcher.Subscribe(validated)
	defer a.watcher.Unsubscribe(sub)

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				a.watcher.Unsubscribe(sub)
				return
			}
		}
	}()

	for event := range sub.Ch {
		if err := conn.WriteJSON(event); err != nil {
			return
		}
	}
}

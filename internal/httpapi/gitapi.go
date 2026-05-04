package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go-webttyd/internal/git"
)

func queryInt(r *http.Request, key string, defaultVal int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return defaultVal
	}
	return n
}

func (a *API) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	project := r.URL.Query().Get("project")
	if project == "" {
		project = a.workspaceRoot
	}

	repo := git.New(project)
	status, err := repo.Status()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if status == nil {
		status = []git.FileStatus{}
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) handleGitLog(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	project := r.URL.Query().Get("project")
	if project == "" {
		project = a.workspaceRoot
	}

	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	repo := git.New(project)
	commits, err := repo.Log(limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if commits == nil {
		commits = []git.Commit{}
	}
	writeJSON(w, http.StatusOK, commits)
}

func (a *API) handleGitBranches(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	project := r.URL.Query().Get("project")
	if project == "" {
		project = a.workspaceRoot
	}

	repo := git.New(project)
	branches, err := repo.Branches()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if branches == nil {
		branches = []git.Branch{}
	}
	writeJSON(w, http.StatusOK, branches)
}

func (a *API) handleGitStage(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string   `json:"project"`
		Paths   []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	project := req.Project
	if project == "" {
		project = a.workspaceRoot
	}

	repo := git.New(project)
	if err := repo.Stage(req.Paths); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleGitUnstage(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string   `json:"project"`
		Paths   []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	project := req.Project
	if project == "" {
		project = a.workspaceRoot
	}

	repo := git.New(project)
	if err := repo.Unstage(req.Paths); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Message string `json:"message"`
		Amend   bool   `json:"amend"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	project := req.Project
	if project == "" {
		project = a.workspaceRoot
	}

	repo := git.New(project)
	if err := repo.Commit(req.Message, req.Amend); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleGitPush(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Remote  string `json:"remote"`
		Branch  string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	project := req.Project
	if project == "" {
		project = a.workspaceRoot
	}

	repo := git.New(project)
	output, err := repo.Push(req.Remote, req.Branch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "output": output})
}

func (a *API) handleGitPull(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Remote  string `json:"remote"`
		Branch  string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	project := req.Project
	if project == "" {
		project = a.workspaceRoot
	}

	repo := git.New(project)
	output, err := repo.Pull(req.Remote, req.Branch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "output": output})
}

func (a *API) handleGitBranch(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Action  string `json:"action"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	project := req.Project
	if project == "" {
		project = a.workspaceRoot
	}

	repo := git.New(project)
	var err error
	switch req.Action {
	case "create":
		err = repo.BranchCreate(req.Name)
	case "switch":
		err = repo.BranchSwitch(req.Name)
	case "delete":
		err = repo.BranchDelete(req.Name)
	default:
		http.Error(w, "invalid action: must be create, switch, or delete", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleGitMerge(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Branch  string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	project := req.Project
	if project == "" {
		project = a.workspaceRoot
	}

	repo := git.New(project)
	if err := repo.Merge(req.Branch); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleGitStash(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string `json:"project"`
		Action  string `json:"action"`
		Index   int    `json:"index"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	project := req.Project
	if project == "" {
		project = a.workspaceRoot
	}

	repo := git.New(project)
	var err error
	switch req.Action {
	case "list":
		stashes, listErr := repo.StashList()
		if listErr != nil {
			http.Error(w, listErr.Error(), http.StatusInternalServerError)
			return
		}
		if stashes == nil {
			stashes = []git.Stash{}
		}
		writeJSON(w, http.StatusOK, stashes)
		return
	case "save":
		err = repo.StashSave(req.Message)
	case "pop":
		err = repo.StashPop(req.Index)
	case "apply":
		err = repo.StashApply(req.Index)
	case "drop":
		err = repo.StashDrop(req.Index)
	default:
		http.Error(w, "invalid action: must be list, save, pop, apply, or drop", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	project := r.URL.Query().Get("project")
	if project == "" {
		project = a.workspaceRoot
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	repo := git.New(project)
	diff, err := repo.Diff(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"diff": diff})
}

func (a *API) handleGitDiffLines(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	project := r.URL.Query().Get("project")
	if project == "" {
		project = a.workspaceRoot
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	repo := git.New(project)
	changes, err := repo.DiffLines(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if changes == nil {
		changes = []git.GutterChange{}
	}
	writeJSON(w, http.StatusOK, changes)
}

func (a *API) handleGitBlame(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	project := r.URL.Query().Get("project")
	if project == "" {
		project = a.workspaceRoot
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	repo := git.New(project)
	blame, err := repo.Blame(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if blame == nil {
		blame = []git.BlameLine{}
	}
	writeJSON(w, http.StatusOK, blame)
}

func (a *API) handleGitDiscard(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Project string   `json:"project"`
		Paths   []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	project := req.Project
	if project == "" {
		project = a.workspaceRoot
	}

	repo := git.New(project)
	if err := repo.Discard(req.Paths); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleGitFileAtHEAD(w http.ResponseWriter, r *http.Request) {
	if !a.requireFullMode(w) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	project := r.URL.Query().Get("project")
	if project == "" {
		project = a.workspaceRoot
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	repo := git.New(project)
	content, err := repo.FileAtHEAD(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Repo provides git operations scoped to a directory.
type Repo struct {
	dir string
}

// New creates a Repo bound to the given directory.
func New(dir string) *Repo {
	return &Repo{dir: dir}
}

// Dir returns the repository working directory.
func (r *Repo) Dir() string {
	return r.dir
}

// IsRepo returns true if the directory is inside a git work tree.
func (r *Repo) IsRepo() bool {
	_, err := r.exec("rev-parse", "--is-inside-work-tree")
	return err == nil
}

// exec runs a git command in the repo directory and returns trimmed stdout.
func (r *Repo) exec(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}

	out := stdout.String()
	out = strings.ReplaceAll(out, "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	return strings.TrimSpace(out), nil
}

// execLines runs a git command and returns output split by newlines (empty lines removed).
func (r *Repo) execLines(args ...string) ([]string, error) {
	out, err := r.exec(args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	lines := strings.Split(out, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			result = append(result, line)
		}
	}
	return result, nil
}

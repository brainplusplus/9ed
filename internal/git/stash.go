package git

import (
	"fmt"
	"strings"
)

// Stash represents a git stash entry.
type Stash struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

// StashList returns all stash entries.
func (r *Repo) StashList() ([]Stash, error) {
	out, err := r.exec("stash", "list", "--format=%gd%n%s%n---")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	entries := strings.Split(out, "---")
	stashes := make([]Stash, 0, len(entries))

	for i, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		lines := strings.Split(entry, "\n")
		msg := ""
		if len(lines) > 1 {
			msg = lines[1]
		}
		stashes = append(stashes, Stash{
			Index:   i,
			Message: msg,
		})
	}

	return stashes, nil
}

// StashSave creates a new stash.
func (r *Repo) StashSave(message string) error {
	args := []string{"stash", "push"}
	if message != "" {
		args = append(args, "-m", message)
	}
	_, err := r.exec(args...)
	return err
}

// StashPop applies and removes a stash.
func (r *Repo) StashPop(index int) error {
	_, err := r.exec("stash", "pop", fmt.Sprintf("stash@{%d}", index))
	return err
}

// StashApply applies a stash without removing it.
func (r *Repo) StashApply(index int) error {
	_, err := r.exec("stash", "apply", fmt.Sprintf("stash@{%d}", index))
	return err
}

// StashDrop removes a stash entry.
func (r *Repo) StashDrop(index int) error {
	_, err := r.exec("stash", "drop", fmt.Sprintf("stash@{%d}", index))
	return err
}

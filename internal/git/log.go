package git

import (
	"fmt"
	"strings"
)

type Commit struct {
	Hash         string `json:"hash"`
	ShortHash    string `json:"shortHash"`
	Message      string `json:"message"`
	Author       string `json:"author"`
	Date         string `json:"date"`
	RelativeDate string `json:"relativeDate"`
}

func (r *Repo) Log(limit, offset int) ([]Commit, error) {
	format := "%H%n%h%n%s%n%an%n%aI%n%ar%n---"
	args := []string{
		"log",
		fmt.Sprintf("--format=%s", format),
		fmt.Sprintf("-n%d", limit),
	}
	if offset > 0 {
		args = append(args, fmt.Sprintf("--skip=%d", offset))
	}

	out, err := r.exec(args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	entries := strings.Split(out, "---")
	commits := make([]Commit, 0, len(entries))

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		lines := strings.Split(entry, "\n")
		if len(lines) < 6 {
			continue
		}
		commits = append(commits, Commit{
			Hash:         lines[0],
			ShortHash:    lines[1],
			Message:      lines[2],
			Author:       lines[3],
			Date:         lines[4],
			RelativeDate: lines[5],
		})
	}

	return commits, nil
}

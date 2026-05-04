package git

import (
	"fmt"
	"strconv"
	"strings"
)

type GutterChange struct {
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Type      string `json:"type"`
}

func (r *Repo) Diff(path string) (string, error) {
	return r.exec("diff", "HEAD", "--", path)
}

func (r *Repo) DiffStaged(path string) (string, error) {
	return r.exec("diff", "--cached", "--", path)
}

func (r *Repo) DiffLines(path string) ([]GutterChange, error) {
	out, err := r.exec("diff", "HEAD", "--unified=0", "--", path)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	return parseDiffHunks(out), nil
}

func (r *Repo) FileAtHEAD(path string) (string, error) {
	return r.exec("show", fmt.Sprintf("HEAD:%s", path))
}

func (r *Repo) DiffCommit(hash string) (string, error) {
	return r.exec("show", "--format=", hash)
}

func parseDiffHunks(diff string) []GutterChange {
	lines := strings.Split(diff, "\n")
	changes := make([]GutterChange, 0)

	for _, line := range lines {
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) < 3 {
			continue
		}

		oldInfo := strings.TrimPrefix(parts[1], "-")
		newInfo := strings.TrimPrefix(parts[2], "+")

		oldCount := parseHunkCount(oldInfo)
		newStart, newCount := parseHunkStartCount(newInfo)

		if oldCount == 0 && newCount > 0 {
			changes = append(changes, GutterChange{
				StartLine: newStart,
				EndLine:   newStart + newCount - 1,
				Type:      "added",
			})
		} else if newCount == 0 && oldCount > 0 {
			changes = append(changes, GutterChange{
				StartLine: newStart,
				EndLine:   newStart,
				Type:      "deleted",
			})
		} else {
			changes = append(changes, GutterChange{
				StartLine: newStart,
				EndLine:   newStart + newCount - 1,
				Type:      "modified",
			})
		}
	}

	return changes
}

func parseHunkCount(info string) int {
	parts := strings.Split(info, ",")
	if len(parts) < 2 {
		return 1
	}
	count, _ := strconv.Atoi(parts[1])
	return count
}

func parseHunkStartCount(info string) (start, count int) {
	parts := strings.Split(info, ",")
	start, _ = strconv.Atoi(parts[0])
	if len(parts) < 2 {
		return start, 1
	}
	count, _ = strconv.Atoi(parts[1])
	return start, count
}

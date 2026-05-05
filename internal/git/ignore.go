package git

import (
	"path/filepath"
	"strings"
)

func (r *Repo) CheckIgnored(paths []string) ([]bool, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	args := append([]string{"check-ignore"}, paths...)
	out, err := r.exec(args...)
	if err != nil {
		return make([]bool, len(paths)), nil
	}

	ignoredSet := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			base := filepath.Base(line)
			ignoredSet[base] = true
			ignoredSet[filepath.ToSlash(line)] = true
		}
	}

	result := make([]bool, len(paths))
	for i, p := range paths {
		if ignoredSet[p] || ignoredSet[filepath.Base(p)] {
			result[i] = true
		}
	}
	return result, nil
}

type RepoFile struct {
	Path    string `json:"path"`
	Ignored bool   `json:"ignored,omitempty"`
}

func (r *Repo) ListFiles(includeIgnored bool) ([]RepoFile, error) {
	tracked, err := r.execLines("ls-files")
	if err != nil {
		return nil, err
	}

	result := make([]RepoFile, 0, len(tracked))
	for _, f := range tracked {
		result = append(result, RepoFile{Path: filepath.ToSlash(f)})
	}

	if includeIgnored {
		othersOut, err := r.execLines("ls-files", "--others", "--ignored", "--exclude-standard")
		if err == nil {
			for _, f := range othersOut {
				result = append(result, RepoFile{Path: filepath.ToSlash(f), Ignored: true})
			}
		}
	}

	return result, nil
}

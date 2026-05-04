package git

import "strings"

type FileStatus struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Staged bool   `json:"staged"`
}

func (r *Repo) Status() ([]FileStatus, error) {
	out, err := r.exec("status", "--porcelain=v1", "-uall")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	lines := strings.Split(out, "\n")
	result := make([]FileStatus, 0, len(lines))

	for _, line := range lines {
		if len(line) < 2 {
			continue
		}
		x := line[0]
		y := line[1]

		var path string
		if len(line) > 3 && line[2] == ' ' {
			path = line[3:]
		} else if len(line) > 2 {
			path = line[2:]
		} else {
			continue
		}
		path = strings.TrimSpace(path)

		if idx := strings.Index(path, " -> "); idx != -1 {
			path = path[idx+4:]
		}

		if x != ' ' && x != '?' {
			result = append(result, FileStatus{
				Path:   path,
				Status: porcelainToStatus(x),
				Staged: true,
			})
		}
		if y != ' ' {
			status := porcelainToStatus(y)
			if x == '?' {
				status = "untracked"
			}
			result = append(result, FileStatus{
				Path:   path,
				Status: status,
				Staged: false,
			})
		}
	}

	return result, nil
}

func (r *Repo) Stage(paths []string) error {
	args := append([]string{"add", "--"}, paths...)
	_, err := r.exec(args...)
	return err
}

func (r *Repo) Unstage(paths []string) error {
	args := append([]string{"reset", "HEAD", "--"}, paths...)
	_, err := r.exec(args...)
	return err
}

func (r *Repo) Discard(paths []string) error {
	args := append([]string{"checkout", "--"}, paths...)
	_, err := r.exec(args...)
	return err
}

func (r *Repo) Commit(message string, amend bool) error {
	args := []string{"commit", "-m", message}
	if amend {
		args = append(args, "--amend")
	}
	_, err := r.exec(args...)
	return err
}

func porcelainToStatus(c byte) string {
	switch c {
	case 'M':
		return "modified"
	case 'A':
		return "added"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case '?':
		return "untracked"
	default:
		return "modified"
	}
}

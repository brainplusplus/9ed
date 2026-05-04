package git

import (
	"strconv"
	"strings"
)

type Branch struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
	Remote  string `json:"remote,omitempty"`
	Ahead   int    `json:"ahead"`
	Behind  int    `json:"behind"`
}

func (r *Repo) Branches() ([]Branch, error) {
	out, err := r.exec("branch", "-vv", "--format=%(HEAD)%(refname:short)%09%(upstream:short)%09%(upstream:track)")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	lines := strings.Split(out, "\n")
	branches := make([]Branch, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}
		current := line[0] == '*'
		rest := line[1:]
		parts := strings.Split(rest, "\t")

		b := Branch{
			Name:    strings.TrimSpace(parts[0]),
			Current: current,
		}

		if len(parts) > 1 {
			b.Remote = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			track := strings.TrimSpace(parts[2])
			b.Ahead, b.Behind = parseTrackInfo(track)
		}

		branches = append(branches, b)
	}

	return branches, nil
}

func (r *Repo) BranchCreate(name string) error {
	_, err := r.exec("branch", name)
	return err
}

func (r *Repo) BranchSwitch(name string) error {
	_, err := r.exec("checkout", name)
	return err
}

func (r *Repo) BranchDelete(name string) error {
	_, err := r.exec("branch", "-d", name)
	return err
}

func (r *Repo) Merge(branch string) error {
	_, err := r.exec("merge", branch)
	return err
}

func parseTrackInfo(track string) (ahead, behind int) {
	track = strings.Trim(track, "[]")
	if track == "" {
		return 0, 0
	}
	parts := strings.Split(track, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "ahead ") {
			ahead, _ = strconv.Atoi(strings.TrimPrefix(p, "ahead "))
		}
		if strings.HasPrefix(p, "behind ") {
			behind, _ = strconv.Atoi(strings.TrimPrefix(p, "behind "))
		}
	}
	return
}

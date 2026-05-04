package git

// Push pushes to a remote.
func (r *Repo) Push(remote, branch string) (string, error) {
	args := []string{"push"}
	if remote != "" {
		args = append(args, remote)
	}
	if branch != "" {
		args = append(args, branch)
	}
	return r.exec(args...)
}

// Pull pulls from a remote.
func (r *Repo) Pull(remote, branch string) (string, error) {
	args := []string{"pull"}
	if remote != "" {
		args = append(args, remote)
	}
	if branch != "" {
		args = append(args, branch)
	}
	return r.exec(args...)
}

// Fetch fetches from all remotes.
func (r *Repo) Fetch() error {
	_, err := r.exec("fetch", "--all")
	return err
}

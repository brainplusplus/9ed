package git

import "strings"

// BlameLine represents a single line of blame output.
type BlameLine struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// Blame returns blame info for a file.
func (r *Repo) Blame(path string) ([]BlameLine, error) {
	out, err := r.exec("blame", "--porcelain", path)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	lines := strings.Split(out, "\n")
	result := make([]BlameLine, 0)
	var current BlameLine
	lineNum := 0

	for _, line := range lines {
		if len(line) >= 40 && line[0] != '\t' && !strings.ContainsAny(line[:1], " \t") {
			// Commit header line: hash origLine finalLine [numLines]
			parts := strings.Fields(line)
			if len(parts) >= 3 && len(parts[0]) == 40 {
				current.Hash = parts[0][:7]
				lineNum++
				current.Line = lineNum
			}
		} else if strings.HasPrefix(line, "author ") {
			current.Author = strings.TrimPrefix(line, "author ")
		} else if strings.HasPrefix(line, "author-time ") {
			current.Date = strings.TrimPrefix(line, "author-time ")
		} else if strings.HasPrefix(line, "\t") {
			current.Content = line[1:]
			result = append(result, current)
			current = BlameLine{}
		}
	}

	return result, nil
}

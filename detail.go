package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Detail is what the list does not carry: fetched for one PR when the cursor
// rests on it.
type Detail struct {
	Body      string `json:"body"`
	Mergeable string `json:"mergeable"`

	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`

	// A user has a login, a team only a name.
	ReviewRequests []struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	} `json:"reviewRequests"`

	LatestReviews []struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		State string `json:"state"`
	} `json:"latestReviews"`

	Comments []json.RawMessage `json:"comments"`
	Commits  []json.RawMessage `json:"commits"`

	Files []struct {
		Path      string `json:"path"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
	} `json:"files"`
}

const detailFields = "body,mergeable,labels,reviewRequests,latestReviews,comments,commits,files"

func FetchDetail(repo string, n int) (Detail, error) {
	var d Detail
	out, err := gh(withRepo([]string{"pr", "view", fmt.Sprint(n), "--json", detailFields}, repo)...)
	if err != nil {
		return d, err
	}
	return d, json.Unmarshal(out, &d)
}

func FetchDiff(repo string, n int) (string, error) {
	out, err := gh(withRepo([]string{"pr", "diff", fmt.Sprint(n), "--color", "never"}, repo)...)
	return string(out), err
}

// deltaTint matches pr-show: syntax colour stays the foreground, with a dark
// tint behind changed lines instead of a block of saturated green.
var deltaTint = []string{
	"--minus-style", "syntax #2d1416",
	"--minus-emph-style", "syntax #5a2124",
	"--plus-style", "syntax #0f2a18",
	"--plus-emph-style", "syntax #1c5232",
	"--zero-style", "syntax",
}

// RenderDiff colours a diff to fit width. dark is passed to delta rather than
// left for it to detect, because detecting asks the terminal, and the terminal
// belongs to the TUI.
func RenderDiff(raw string, width int, dark bool) string {
	if path, err := exec.LookPath("delta"); err == nil {
		args := []string{"--paging=never", "--line-numbers", "--width", fmt.Sprint(width),
			"--wrap-max-lines", "3", "--hunk-header-decoration-style", "none"}
		if dark {
			args = append(append(args, "--dark"), deltaTint...)
		} else {
			args = append(args, "--light")
		}
		cmd := exec.Command(path, args...)
		cmd.Stdin = strings.NewReader(raw)
		if out, err := cmd.Output(); err == nil {
			return string(out)
		}
	}

	var b strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git"):
			line = bold.Render(line)
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"), strings.HasPrefix(line, "index "):
			line = dim.Render(line)
		case strings.HasPrefix(line, "@@"):
			line = accent.Render(line)
		case strings.HasPrefix(line, "+"):
			line = green.Render(line)
		case strings.HasPrefix(line, "-"):
			line = red.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

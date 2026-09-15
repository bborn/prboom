package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type PR struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	Additions      int    `json:"additions"`
	Deletions      int    `json:"deletions"`
	ChangedFiles   int    `json:"changedFiles"`
	IsDraft        bool   `json:"isDraft"`
	UpdatedAt      string `json:"updatedAt"`
	BaseRefName    string `json:"baseRefName"`
	HeadRefName    string `json:"headRefName"`
	ReviewDecision string `json:"reviewDecision"`

	Author struct {
		Login string `json:"login"`
	} `json:"author"`

	StatusCheckRollup []struct {
		State      string `json:"state"`
		Conclusion string `json:"conclusion"`
	} `json:"statusCheckRollup"`

	IsCrossRepository bool `json:"isCrossRepository"`

	// Derived, so a cached list gets them back through derive.
	Checks  string `json:"-"`
	AgeDays int    `json:"-"`
}

const ghFields = "number,title,author,additions,deletions,changedFiles,updatedAt," +
	"isDraft,statusCheckRollup,url,headRefName,baseRefName,isCrossRepository,reviewDecision"

// Scopes are the lists 1, 2 and 3 switch between, in that order.
var scopes = []struct{ Key, Label string }{
	{"review", "review requested"},
	{"all", "all open"},
	{"mine", "mine"},
}

// Fetch shells out to gh for one scope. author, if set, narrows any scope to
// PRs by that login, and stands in for @me on "mine".
func Fetch(repo, scope, author string, limit int) ([]PR, error) {
	args := withRepo([]string{"pr", "list", "--state", "open",
		"--limit", fmt.Sprint(limit), "--json", ghFields}, repo)
	switch scope {
	case "review":
		args = append(args, "--search", "review-requested:@me")
	case "mine":
		if author == "" {
			author = "@me"
		}
	}
	if author != "" {
		args = append(args, "--author", author)
	}

	out, err := gh(args...)
	if err != nil {
		return nil, err
	}
	var prs []PR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, err
	}
	for i := range prs {
		prs[i].derive()
	}
	return prs, nil
}

// gh runs gh and returns its output, with gh's own complaint as the error.
func gh(args ...string) ([]byte, error) {
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("gh: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

func withRepo(args []string, repo string) []string {
	if repo != "" {
		return append(args, "-R", repo)
	}
	return args
}

// Sorts, in the order s cycles through them.
var sorts = []string{"recency", "author", "loc", "files"}

// sortPRs orders in place. Judging them is your job, not the tool's; this only
// changes where to look first. Ties fall back to most recently touched.
func sortPRs(prs []PR, by string) {
	sort.SliceStable(prs, func(a, b int) bool {
		p, q := prs[a], prs[b]
		switch by {
		case "author":
			if p.Author.Login != q.Author.Login {
				return strings.ToLower(p.Author.Login) < strings.ToLower(q.Author.Login)
			}
		case "loc":
			if pl, ql := p.Additions+p.Deletions, q.Additions+q.Deletions; pl != ql {
				return pl > ql
			}
		case "files":
			if p.ChangedFiles != q.ChangedFiles {
				return p.ChangedFiles > q.ChangedFiles
			}
		}
		return p.UpdatedAt > q.UpdatedAt
	})
}

// matches is the / filter: every word must appear in the number, title,
// author or branch.
func matches(p PR, query string) bool {
	hay := strings.ToLower(fmt.Sprintf("#%d %s %s %s", p.Number, p.Title, p.Author.Login, p.HeadRefName))
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(hay, word) {
			return false
		}
	}
	return true
}

func (p *PR) derive() {
	p.Checks = p.checksState()
	p.AgeDays = ageDays(p.UpdatedAt)
}

func (p *PR) checksState() string {
	if len(p.StatusCheckRollup) == 0 {
		return "none"
	}
	pending := false
	for _, c := range p.StatusCheckRollup {
		s := c.Conclusion
		if s == "" {
			s = c.State
		}
		switch s {
		case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED":
			return "red"
		case "PENDING", "IN_PROGRESS", "QUEUED", "":
			pending = true
		}
	}
	if pending {
		return "pending"
	}
	return "green"
}

func ageDays(iso string) int {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return 0
	}
	return int(time.Since(t).Hours() / 24)
}

// ago is how long since iso, in the fewest characters that still read.
func ago(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	return short(time.Since(t))
}

func short(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return fmt.Sprintf("%dw", int(d.Hours()/24/7))
}

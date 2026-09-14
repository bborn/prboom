package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Review is a walkthrough already under way for a PR, made by either path.
type Review struct {
	TaskID     int    // TaskYou task, 0 if none
	TaskStatus string // its status
	Local      bool   // a pr-open worktree or session
}

func (r Review) Label() string {
	if r.TaskID != 0 {
		return fmt.Sprintf("ty %d", r.TaskID)
	}
	return "open"
}

func (r Review) Detail() string {
	var parts []string
	if r.TaskID != 0 {
		parts = append(parts, fmt.Sprintf("task #%d %s", r.TaskID, r.TaskStatus))
	}
	if r.Local {
		parts = append(parts, "pr-open worktree")
	}
	return "reviewing: " + strings.Join(parts, " + ")
}

var reviewTitle = regexp.MustCompile(`^Review PR #(\d+):`)

// Reviews finds walkthroughs in progress for the repo you are in, keyed by PR
// number. Scoped to that repo so another repo's PR #12 does not light up this
// one's. Every failure just means nothing is marked.
func Reviews() map[int]Review {
	out := map[int]Review{}
	common, err := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return out
	}
	main := filepath.Dir(strings.TrimSpace(string(common)))

	localReviews(main, out)
	taskReviews(main, out)
	return out
}

// pr-open always checks a PR out on branch pr/N, so the worktree list says
// which ones are open without knowing where the worktrees were put.
func localReviews(main string, out map[int]Review) {
	b, err := exec.Command("git", "-C", main, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		n, ok := strings.CutPrefix(sc.Text(), "branch refs/heads/pr/")
		if !ok {
			continue
		}
		if num, err := strconv.Atoi(n); err == nil {
			r := out[num]
			r.Local = true
			out[num] = r
		}
	}
}

// pr-task titles every task "Review PR #N: ...". Only unfinished tasks count;
// ty list leaves done ones out by default.
func taskReviews(main string, out map[int]Review) {
	if _, err := exec.LookPath("ty"); err != nil {
		return
	}
	b, err := exec.Command("ty", "projects", "list", "--json").Output()
	if err != nil {
		return
	}
	var projects []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if json.Unmarshal(b, &projects) != nil {
		return
	}
	project := ""
	for _, p := range projects {
		if filepath.Clean(p.Path) == main {
			project = p.Name
			break
		}
	}
	if project == "" {
		return
	}

	b, err = exec.Command("ty", "list", "--json", "--project", project, "--limit", "500").Output()
	if err != nil {
		return
	}
	var tasks []struct {
		ID     int    `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	if json.Unmarshal(b, &tasks) != nil {
		return
	}
	for _, t := range tasks {
		m := reviewTitle.FindStringSubmatch(t.Title)
		if m == nil {
			continue
		}
		num, _ := strconv.Atoi(m[1])
		r := out[num]
		// ty lists newest first; keep the newest if there are several.
		if r.TaskID == 0 {
			r.TaskID, r.TaskStatus = t.ID, t.Status
		}
		out[num] = r
	}
}

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
	Number       int    `json:"number"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	ChangedFiles int    `json:"changedFiles"`
	IsDraft      bool   `json:"isDraft"`
	UpdatedAt    string `json:"updatedAt"`
	BaseRefName  string `json:"baseRefName"`
	HeadRefName  string `json:"headRefName"`

	Author struct {
		Login string `json:"login"`
	} `json:"author"`

	StatusCheckRollup []struct {
		State      string `json:"state"`
		Conclusion string `json:"conclusion"`
	} `json:"statusCheckRollup"`

	IsCrossRepository bool `json:"isCrossRepository"`

	// Derived.
	Checks  string `json:"-"`
	AgeDays int    `json:"-"`
}

const ghFields = "number,title,author,additions,deletions,changedFiles,updatedAt," +
	"isDraft,statusCheckRollup,url,headRefName,baseRefName,isCrossRepository"

// Fetch shells out to gh. mine limits to PRs waiting on the user.
func Fetch(repo string, mine bool, limit int) ([]PR, error) {
	args := []string{"pr", "list", "--state", "open", "--limit", fmt.Sprint(limit), "--json", ghFields}
	if repo != "" {
		args = append([]string{"pr", "list", "-R", repo}, args[2:]...)
	}
	if mine {
		args = append(args, "--search", "review-requested:@me")
	}

	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("gh: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}

	var prs []PR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, err
	}
	for i := range prs {
		prs[i].Checks = prs[i].checksState()
		prs[i].AgeDays = ageDays(prs[i].UpdatedAt)
	}
	// Most recently touched first. Judging them is your job, not the tool's.
	sort.SliceStable(prs, func(a, b int) bool { return prs[a].UpdatedAt > prs[b].UpdatedAt })
	return prs, nil
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

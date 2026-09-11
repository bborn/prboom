package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Verdicts, in the order a reviewer should work them.
const (
	VerdictStandard   = "STANDARD"
	VerdictMechanical = "MECHANICAL"
	VerdictHeavy      = "HEAVY"
	VerdictNeedsPlan  = "NEEDS-PLAN"
	VerdictBlocked    = "BLOCKED"
)

var verdictOrder = map[string]int{
	VerdictStandard:   0,
	VerdictMechanical: 1,
	VerdictHeavy:      2,
	VerdictNeedsPlan:  3,
	VerdictBlocked:    4,
}

const (
	heavyAdditions = 1500
	heavyFiles     = 40
	thinBody       = 120
)

var generated = regexp.MustCompile(
	`(package-lock\.json|yarn\.lock|Gemfile\.lock|poetry\.lock|pnpm-lock\.yaml|` +
		`go\.sum|Cargo\.lock|/vendor/|\.min\.(js|css)$|_pb2?\.py$|\.generated\.|` +
		`schema\.rb$|structure\.sql$|/dist/|/build/)`)

type PR struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	URL          string `json:"url"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	ChangedFiles int    `json:"changedFiles"`
	IsDraft      bool   `json:"isDraft"`
	CreatedAt    string `json:"createdAt"`
	BaseRefName  string `json:"baseRefName"`
	HeadRefName  string `json:"headRefName"`

	Author struct {
		Login string `json:"login"`
	} `json:"author"`

	Files []struct {
		Path string `json:"path"`
	} `json:"files"`

	StatusCheckRollup []struct {
		State      string `json:"state"`
		Conclusion string `json:"conclusion"`
	} `json:"statusCheckRollup"`

	// Derived.
	Verdict string `json:"-"`
	Why     string `json:"-"`
	Checks  string `json:"-"`
	AgeDays int    `json:"-"`
}

const ghFields = "number,title,author,additions,deletions,changedFiles,createdAt," +
	"isDraft,statusCheckRollup,reviewDecision,body,url,headRefName,baseRefName,files"

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
		prs[i].classify()
	}
	sort.SliceStable(prs, func(a, b int) bool {
		oa, ob := verdictOrder[prs[a].Verdict], verdictOrder[prs[b].Verdict]
		if oa != ob {
			return oa < ob
		}
		return prs[a].Additions > prs[b].Additions
	})
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

func (p *PR) classify() {
	p.Checks = p.checksState()
	p.AgeDays = ageDays(p.CreatedAt)

	if p.Checks == "red" {
		p.Verdict, p.Why = VerdictBlocked, "CI red"
		return
	}
	if len(strings.TrimSpace(p.Body)) < thinBody {
		p.Verdict = VerdictNeedsPlan
		p.Why = fmt.Sprintf("%d char description", len(strings.TrimSpace(p.Body)))
		return
	}
	if len(p.Files) > 0 {
		allGenerated := true
		for _, f := range p.Files {
			if !generated.MatchString(f.Path) {
				allGenerated = false
				break
			}
		}
		if allGenerated {
			p.Verdict, p.Why = VerdictMechanical, "generated files only"
			return
		}
	}
	if p.Additions > heavyAdditions || p.ChangedFiles > heavyFiles {
		p.Verdict = VerdictHeavy
		p.Why = fmt.Sprintf("+%d across %d files", p.Additions, p.ChangedFiles)
		return
	}
	// Nothing to say: the size columns already carry it.
	p.Verdict, p.Why = VerdictStandard, ""
}

func ageDays(iso string) int {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return 0
	}
	return int(time.Since(t).Hours() / 24)
}

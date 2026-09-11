// prboom: pick a pull request worth your time, then get to work on it.
//
// Run it inside any git repo. It lists open PRs with a verdict on each,
// you arrow to one, and Enter checks it out and writes the brief that the
// iTerm2 PR Review workgroup reads.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

// workspaceProfile is the iTerm2 profile carrying the PR Review workgroup trigger.
const workspaceProfile = "PR Chat"

// openingPrompt is what Claude starts on, so the session lands already working
// the PR rather than at a blank cursor.
const openingPrompt = "/pr-cleanup"

var (
	bare   *bool
	prompt *string
)

func main() {
	repo := flag.String("R", "", "owner/name, defaults to the repo you are in")
	all := flag.Bool("a", false, "every open PR, not just ones awaiting your review")
	limit := flag.Int("n", 60, "how many to fetch")
	plain := flag.Bool("l", false, "print the list and exit, no picker")
	bare = flag.Bool("bare", false, "check out only, do not start the review workspace")
	prompt = flag.String("p", openingPrompt, "what Claude starts on; empty for a blank session")
	flag.Parse()

	if *repo == "" && !inGitRepo() {
		fmt.Fprintln(os.Stderr, "prboom: not inside a git repository (use -R owner/name)")
		os.Exit(1)
	}
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(os.Stderr, "prboom: gh is not installed")
		os.Exit(1)
	}

	if *plain {
		prs, err := Fetch(*repo, !*all, *limit)
		if err != nil {
			fmt.Fprintln(os.Stderr, "prboom:", err)
			os.Exit(1)
		}
		printPlain(prs)
		return
	}

	// No alt screen: the list renders inline and leaves the terminal as it was.
	p := tea.NewProgram(newModel(*repo, !*all, *limit))
	final, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "prboom:", err)
		os.Exit(1)
	}

	m, ok := final.(model)
	if !ok {
		return
	}
	switch m.action.Kind {
	case "start":
		// pr-start builds a worktree and tells us where it put it.
		f, err := os.CreateTemp("", "prboom-path")
		if err == nil {
			f.Close()
			defer os.Remove(f.Name())
			os.Setenv("PR_START_PATH_OUT", f.Name())
		}
		run("pr-start", fmt.Sprint(m.action.PR.Number))
		if *bare {
			return
		}
		tree := ""
		if f != nil {
			if b, err := os.ReadFile(f.Name()); err == nil {
				tree = strings.TrimSpace(string(b))
			}
		}
		enterWorkspace(tree)
	case "diff":
		// Show the PR's diff without checking it out, so triage stays cheap.
		run("sh", "-c", fmt.Sprintf(
			`gh pr diff %d | delta --paging=always --navigate --line-numbers --hyperlinks `+
				`--hyperlinks-file-link-format "file://{path}#{line}" --side-by-side`,
			m.action.PR.Number))
	}
}

func printPlain(prs []PR) {
	if len(prs) == 0 {
		fmt.Println("nothing waiting on you.")
		return
	}
	counts := map[string]int{}
	for _, p := range prs {
		counts[p.Verdict]++
		why := ""
		if p.Why != "" {
			why = " · " + p.Why
		}
		fmt.Printf("%s %s %-16s %-58s +%d/-%d · %df · %dd%s\n",
			pad(p.Verdict, 10), pad("#"+fmt.Sprint(p.Number), 6),
			truncate(p.Author.Login, 16), truncate(p.Title, 58),
			p.Additions, p.Deletions, p.ChangedFiles, p.AgeDays, why)
	}
	fmt.Println()
	for _, v := range []string{VerdictStandard, VerdictMechanical, VerdictHeavy, VerdictNeedsPlan, VerdictBlocked} {
		if counts[v] > 0 {
			fmt.Printf("  %s %d", v, counts[v])
		}
	}
	fmt.Println()
}

// enterWorkspace turns this session into the PR Review workgroup and hands it
// to Claude. The profile carries the Enter Workgroup trigger, so switching to
// it before claude starts is what makes the Diff, Cut and PR peers appear
// instead of the plain Claude Code workgroup.
//
// It replaces this process, so prboom never returns. If anything is missing we
// still start claude; you just get the default workgroup.
func enterWorkspace(tree string) {
	claude, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(os.Stderr, "prboom: claude is not on PATH")
		os.Exit(1)
	}

	// Start on the job, not at a blank prompt. The skill reads the brief and
	// the diff stat that pr-start just wrote, then produces the cut list.
	launch := "claude"
	if *prompt != "" {
		launch += " " + shellQuote(*prompt)
	}
	cmd := launch
	if tree != "" {
		cmd = "cd " + shellQuote(tree) + " && " + launch
	}

	it2, it2err := exec.LookPath("it2")
	sid := sessionID()

	if it2err == nil && sid != "" {
		if err := exec.Command(it2, "profile", "apply", workspaceProfile, "-s", sid).Run(); err == nil {
			// Queue the command on the tty so the shell runs it once we exit.
			// A shell-launched process is what the profile's Job Started
			// trigger watches for; exec'ing in place keeps the same pid and
			// the trigger can miss it.
			if err := exec.Command(it2, "session", "run", cmd, "-s", sid).Run(); err == nil {
				return
			}
		}
	}

	// No iTerm2 to talk to: start claude here. You get the default workgroup.
	if tree != "" {
		_ = os.Chdir(tree)
	}
	if err := syscall.Exec(claude, []string{"claude"}, os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "prboom:", err)
		os.Exit(1)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sessionID is the uuid half of ITERM_SESSION_ID (w0t0p0:UUID).
func sessionID() string {
	v := os.Getenv("ITERM_SESSION_ID")
	if i := strings.LastIndex(v, ":"); i >= 0 {
		return v[i+1:]
	}
	return v
}

func inGitRepo() bool {
	return exec.Command("git", "rev-parse", "--git-dir").Run() == nil
}

// run hands the terminal to another command and exits with its status.
func run(name string, args ...string) {
	path, err := exec.LookPath(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prboom: %s not found on PATH\n", name)
		os.Exit(1)
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "prboom:", err)
		os.Exit(1)
	}
}

func openBrowser(url string) {
	if url == "" {
		return
	}
	_ = exec.Command("open", url).Start()
}

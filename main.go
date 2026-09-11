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

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	repo := flag.String("R", "", "owner/name, defaults to the repo you are in")
	all := flag.Bool("a", false, "every open PR, not just ones awaiting your review")
	limit := flag.Int("n", 60, "how many to fetch")
	plain := flag.Bool("l", false, "print the list and exit, no picker")
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
		run("pr-start", fmt.Sprint(m.action.PR.Number))
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

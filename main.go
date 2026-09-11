// prboom: pick a pull request and walk it one finding at a time.
//
// Run it inside any git repo. Arrow to a PR and press Enter: pr-open makes a
// worktree and a tmux window with the agent beside a Shell pane, using nothing
// but git and tmux. Press t instead to track it on the TaskYou board, which
// gives the same two panes.
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
	args := []string{fmt.Sprint(m.action.PR.Number)}
	if *repo != "" {
		args = append(args, "-R", *repo)
	}
	switch m.action.Kind {
	case "open":
		// git + tmux only. Nothing else is required to walk a PR.
		run("pr-open", args...)
	case "task":
		// Same two-pane shape, but tracked on the TaskYou board.
		run("pr-task", args...)
	case "diff":
		// Read the diff without making a task of it.
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
	for _, p := range prs {
		fmt.Printf("%s %-16s %-60s +%d/-%d · %df · %dd · %s\n",
			pad("#"+fmt.Sprint(p.Number), 6), truncate(p.Author.Login, 16),
			truncate(p.Title, 60), p.Additions, p.Deletions,
			p.ChangedFiles, p.AgeDays, p.Checks)
	}
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

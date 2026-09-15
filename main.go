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
	"slices"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func main() {
	repo := flag.String("R", "", "owner/name, defaults to the repo you are in")
	all := flag.Bool("a", false, "start on every open PR, not just ones awaiting your review")
	author := flag.String("A", "", "only PRs by this author (login, or @me)")
	sortBy := flag.String("s", "recency", "sort by: recency, author, loc, files")
	limit := flag.Int("n", 60, "how many to fetch")
	plain := flag.Bool("l", false, "print the list and exit, no picker")
	agent := flag.String("agent", "", "coding agent: claude, codex, gemini, grok, cursor, opencode")
	link := flag.Bool("link-skill", false, "install the pr-walk skill into your Claude config dirs, then exit")
	ty := flag.Bool("ty", false, "with a PR number: walk it as a TaskYou task, opened in ty")
	flag.Parse()

	// prboom 1234: no picker, walk that PR. Flags may come after the number,
	// which the flag package stops at, so parse what follows it too.
	target := 0
	if rest := flag.Args(); len(rest) > 0 {
		n, err := strconv.Atoi(strings.TrimPrefix(rest[0], "#"))
		if err != nil || n <= 0 {
			fmt.Fprintf(os.Stderr, "prboom: %q is not a PR number\n", rest[0])
			os.Exit(2)
		}
		target = n
		_ = flag.CommandLine.Parse(rest[1:])
		if flag.NArg() > 0 {
			fmt.Fprintf(os.Stderr, "prboom: unexpected %q\n", flag.Arg(0))
			os.Exit(2)
		}
	}
	if *ty && target == 0 {
		fmt.Fprintln(os.Stderr, "prboom: -ty needs a PR number: prboom 1234 -ty")
		os.Exit(2)
	}
	given := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { given[f.Name] = true })

	if *link {
		if err := linkSkill(); err != nil {
			fmt.Fprintln(os.Stderr, "prboom:", err)
			os.Exit(1)
		}
		return
	}

	if !slices.Contains(sorts, *sortBy) {
		fmt.Fprintf(os.Stderr, "prboom: -s must be one of %s\n", strings.Join(sorts, ", "))
		os.Exit(2)
	}

	if *repo == "" && !inGitRepo() {
		fmt.Fprintln(os.Stderr, "prboom: not inside a git repository (use -R owner/name)")
		os.Exit(1)
	}
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(os.Stderr, "prboom: gh is not installed")
		os.Exit(1)
	}

	if target != 0 {
		kind := "open"
		if *ty {
			kind = "task"
		}
		// pr-task finds an existing task for the PR itself, so no lookup here.
		name, args := command(kind, target, options{repo: *repo, agent: *agent}, nil)
		path, err := exec.LookPath(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "prboom: %s is not on PATH\n", name)
			os.Exit(1)
		}
		err = syscall.Exec(path, append([]string{name}, args...), os.Environ())
		fmt.Fprintln(os.Stderr, "prboom:", err)
		os.Exit(1)
	}

	scope := "review"
	if *all {
		scope = "all"
	}

	if *plain {
		prs, err := Fetch(*repo, scope, *author, *limit)
		if err != nil {
			fmt.Fprintln(os.Stderr, "prboom:", err)
			os.Exit(1)
		}
		sortPRs(prs, *sortBy)
		printPlain(prs, reviewsFor(*repo), scope)
		return
	}

	// Sweep finished PRs in the background. Nobody remembers to tidy up, so
	// this is the only way the sessions and worktrees do not accumulate. It
	// only ever touches what pr-open made, never a TaskYou worktree, and never
	// one with uncommitted changes.
	if sweep, err := exec.LookPath("pr-close"); err == nil {
		_ = exec.Command(sweep, "--stale").Start()
	}

	// Asked once, before the TUI owns the terminal: the markdown and diff
	// renderers would otherwise each query it mid-frame.
	dark := lipgloss.HasDarkBackground()

	o := options{
		repo: *repo, scope: scope, author: *author, sortBy: *sortBy,
		agent: *agent, limit: *limit, dark: dark,
	}
	// Open the way this repo was left, unless a flag asks for something else.
	statePath := stateFile(*repo)
	if s, ok := readState(statePath); ok {
		s.apply(&o, given)
	}

	p := tea.NewProgram(newModel(o), tea.WithAltScreen())
	final, err := p.Run()
	if fm, ok := final.(model); ok {
		writeState(statePath, fm.state())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "prboom:", err)
		os.Exit(1)
	}
}

func printPlain(prs []PR, reviews map[int]Review, scope string) {
	if len(prs) == 0 {
		for _, s := range scopes {
			if s.Key == scope {
				fmt.Printf("nothing in %s.\n", s.Label)
			}
		}
		return
	}
	for _, p := range prs {
		label := ""
		if r, ok := reviews[p.Number]; ok {
			label = r.Label()
		}
		fmt.Printf("%s %-8s %-16s %-60s +%d/-%d · %df · %dd · %s\n",
			pad("#"+fmt.Sprint(p.Number), 6), label, truncate(p.Author.Login, 16),
			truncate(p.Title, 60), p.Additions, p.Deletions,
			p.ChangedFiles, p.AgeDays, p.Checks)
	}
}

func inGitRepo() bool {
	return exec.Command("git", "rev-parse", "--git-dir").Run() == nil
}

func openBrowser(url string) {
	if url == "" {
		return
	}
	_ = exec.Command("open", url).Start()
}

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// A walk's shared state lives in the worktree's own git dir, under prboom/.
// The agent writes to it with prboom show / findings / comment, and the code
// pane (prboom view) reads it. A file, not a socket: nothing has to be
// listening when the agent speaks, a pane started later picks up where the
// walk is, and removing the worktree removes all of it.
//
//	view.json      what the agent last asked to show
//	findings.json  the walk's list, worst first
//	comments.json  notes for the author, from you (c in the pane) or the agent
//	view.pid       the running pane, so show knows whether to start one
type showReq struct {
	Path string    `json:"path"`
	Line int       `json:"line,omitempty"`
	Ctx  int       `json:"ctx,omitempty"`
	At   time.Time `json:"at"`
}

type finding struct {
	Path  string `json:"path"`
	Line  int    `json:"line,omitempty"`
	Title string `json:"title"`
}

// A note for the PR author, anchored where GitHub anchors review comments: a
// line on the RIGHT (new) side, or the LEFT side for a deleted line.
type note struct {
	Path string    `json:"path"`
	Line int       `json:"line"`
	Side string    `json:"side"`
	Body string    `json:"body"`
	Code string    `json:"code,omitempty"` // the line as it read when noted
	By   string    `json:"by"`             // you or agent
	At   time.Time `json:"at"`
}

func (f finding) loc() string { return loc(f.Path, f.Line) }
func (n note) loc() string    { return loc(n.Path, n.Line) }

func loc(path string, line int) string {
	if line > 0 {
		return fmt.Sprintf("%s:%d", path, line)
	}
	return path
}

func walkDir(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", errors.New("not inside a git repository")
	}
	return filepath.Join(strings.TrimSpace(out), "prboom"), nil
}

func gitRoot(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", errors.New("not inside a git repository")
	}
	return strings.TrimSpace(out), nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}

func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(b, v) == nil
}

func readComments(dir string) []note {
	var ns []note
	readJSON(filepath.Join(dir, "comments.json"), &ns)
	return ns
}

// setComment adds, replaces (same place, same author) or, with an empty body,
// removes a note.
func setComment(dir string, n note) []note {
	ns := slices.DeleteFunc(readComments(dir), func(o note) bool {
		return o.Path == n.Path && o.Line == n.Line && o.Side == n.Side && o.By == n.By
	})
	if strings.TrimSpace(n.Body) != "" {
		ns = append(ns, n)
	}
	slices.SortStableFunc(ns, func(a, b note) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return strings.Compare(a.Side, b.Side) // LEFT, the deleted line, first
	})
	writeJSON(filepath.Join(dir, "comments.json"), ns)
	return ns
}

// parseLoc splits FILE:LINE. A path with no line, or a line that is not a
// number, is all path.
func parseLoc(s string) (string, int) {
	if i := strings.LastIndex(s, ":"); i > 0 {
		if n, err := strconv.Atoi(s[i+1:]); err == nil && n > 0 {
			return s[:i], n
		}
	}
	return s, 0
}

// resolvePath turns what the agent typed into a path relative to the repo
// root: absolute, relative to where it stands, relative to the root, or the
// tail of a tracked path (offer.rb for app/models/offer.rb).
func resolvePath(root, cwd, p string) (string, error) {
	try := func(abs string) (string, bool) {
		if st, err := os.Stat(abs); err == nil && !st.IsDir() {
			if rel, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(rel, "..") {
				return rel, true
			}
		}
		return "", false
	}
	if filepath.IsAbs(p) {
		if rel, ok := try(p); ok {
			return rel, nil
		}
	} else {
		if rel, ok := try(filepath.Join(root, p)); ok {
			return rel, nil
		}
		if rel, ok := try(filepath.Join(cwd, p)); ok {
			return rel, nil
		}
	}
	// A deleted file is not on disk but is still worth showing.
	if out, err := git(root, "ls-files", "--", "*"+strings.TrimPrefix(p, "./")); err == nil {
		if first, _, _ := strings.Cut(out, "\n"); first != "" {
			return first, nil
		}
	}
	if out, err := git(root, "diff", "--name-only", reviewBase(root), "--", "*"+p); err == nil {
		if first, _, _ := strings.Cut(out, "\n"); first != "" {
			return first, nil
		}
	}
	return "", fmt.Errorf("cannot find %s", p)
}

// subcommand runs prboom view / show / findings / comment / comments, and
// reports whether args named one.
func subcommand(args []string) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}
	run := map[string]func([]string) error{
		"view":     runView,
		"show":     runShow,
		"findings": runFindings,
		"comment":  runComment,
		"comments": runComments,
	}[args[0]]
	if run == nil {
		return false, 0
	}
	if err := run(args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "prboom %s: %v\n", args[0], err)
		return true, 1
	}
	return true, 0
}

// prboom show FILE[:LINE] [CTX]
func runShow(args []string) error {
	if len(args) == 0 || len(args) > 2 {
		return errors.New("usage: prboom show FILE[:LINE] [CONTEXT]")
	}
	cwd, _ := os.Getwd()
	root, err := gitRoot(cwd)
	if err != nil {
		return err
	}
	dir, err := walkDir(cwd)
	if err != nil {
		return err
	}
	p, line := parseLoc(args[0])
	rel, err := resolvePath(root, cwd, p)
	if err != nil {
		return err
	}
	req := showReq{Path: rel, Line: line, At: time.Now()}
	if len(args) == 2 {
		if req.Ctx, err = strconv.Atoi(args[1]); err != nil || req.Ctx < 0 {
			return fmt.Errorf("context %q is not a number", args[1])
		}
	}
	writeJSON(filepath.Join(dir, "view.json"), req)
	msg, err := ensureViewer(dir, root)
	if err != nil {
		return err
	}
	fmt.Println(msg)
	return nil
}

// prboom findings < list, one per line: FILE:LINE title
func runFindings(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: prboom findings < list (FILE:LINE title, one per line)")
	}
	return runFindingsFrom(os.Stdin)
}

func runFindingsFrom(r io.Reader) error {
	cwd, _ := os.Getwd()
	root, err := gitRoot(cwd)
	if err != nil {
		return err
	}
	dir, err := walkDir(cwd)
	if err != nil {
		return err
	}
	fs, err := parseFindings(r, func(p string) string {
		if rel, err := resolvePath(root, cwd, p); err == nil {
			return rel
		}
		return p
	})
	if err != nil {
		return err
	}
	writeJSON(filepath.Join(dir, "findings.json"), fs)
	fmt.Printf("%s in the code pane\n", plural(len(fs), "finding"))
	return nil
}

func parseFindings(r io.Reader, resolve func(string) string) ([]finding, error) {
	fs := []finding{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		where, title, _ := strings.Cut(text, " ")
		p, line := parseLoc(where)
		fs = append(fs, finding{Path: resolve(p), Line: line, Title: strings.TrimSpace(title)})
	}
	return fs, sc.Err()
}

// prboom comment [-left] FILE:LINE TEXT...
func runComment(args []string) error {
	side := "RIGHT"
	if len(args) > 0 && args[0] == "-left" {
		side, args = "LEFT", args[1:]
	}
	if len(args) < 2 {
		return errors.New("usage: prboom comment [-left] FILE:LINE TEXT (empty TEXT removes it)")
	}
	cwd, _ := os.Getwd()
	root, err := gitRoot(cwd)
	if err != nil {
		return err
	}
	dir, err := walkDir(cwd)
	if err != nil {
		return err
	}
	p, line := parseLoc(args[0])
	if line == 0 {
		return errors.New("a comment needs a line: FILE:LINE")
	}
	rel, err := resolvePath(root, cwd, p)
	if err != nil {
		return err
	}
	n := note{Path: rel, Line: line, Side: side, Body: strings.Join(args[1:], " "), By: "agent", At: time.Now()}
	n.Code = lineText(root, rel, line, side)
	ns := setComment(dir, n)
	fmt.Printf("%s now\n", plural(len(ns), "comment"))
	return nil
}

// prboom comments [--json | --clear]
func runComments(args []string) error {
	cwd, _ := os.Getwd()
	dir, err := walkDir(cwd)
	if err != nil {
		return err
	}
	ns := readComments(dir)
	switch {
	case len(args) == 0:
		for _, n := range ns {
			fmt.Printf("%s  (%s)\n%s\n\n", n.loc(), n.By, n.Body)
		}
		if len(ns) == 0 {
			fmt.Println("no comments")
		}
	case args[0] == "--json":
		if ns == nil {
			ns = []note{}
		}
		b, _ := json.MarshalIndent(ns, "", "  ")
		fmt.Println(string(b))
	case args[0] == "--clear":
		return os.Remove(filepath.Join(dir, "comments.json"))
	default:
		return errors.New("usage: prboom comments [--json | --clear]")
	}
	return nil
}

// lineText is the line a note points at: from the worktree for the new side,
// from the review base for a deleted line.
func lineText(root, rel string, line int, side string) string {
	var src string
	if side == "LEFT" {
		out, err := git(root, "show", mergeBase(root)+":"+rel)
		if err != nil {
			return ""
		}
		src = out
	} else {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return ""
		}
		src = string(b)
	}
	lines := strings.Split(src, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}

// ensureViewer makes sure a code pane is running for this worktree. It never
// types into a shell: an idle Shell pane is replaced by the viewer outright,
// and a pane that is busy running something is left alone.
func ensureViewer(dir, root string) (string, error) {
	if viewerAlive(dir) {
		return "shown in the code pane", nil
	}
	if os.Getenv("TMUX") == "" || os.Getenv("TMUX_PANE") == "" {
		return "", errors.New("no code pane: not inside tmux. Run `prboom view` in a terminal here")
	}
	target, cmd, err := shellPane()
	if err != nil {
		return "", err
	}
	if !slices.Contains(shells, cmd) {
		return "", fmt.Errorf("the Shell pane is busy running %s; it will show when you run `prboom view` there", cmd)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// When the viewer quits, the pane becomes a shell again rather than closing.
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	line := fmt.Sprintf("%s view; exec %s -l", shellQuote(exe), shellQuote(sh))
	if _, err := tmux("respawn-pane", "-k", "-t", target, "-c", root, line); err != nil {
		return "", fmt.Errorf("could not start the code pane: %v", err)
	}
	_, _ = tmux("select-pane", "-t", target, "-T", "Shell")
	return "opened the code pane", nil
}

var shells = []string{"zsh", "bash", "fish", "sh", "dash", "nu", "-zsh", "-bash", "-fish", "-sh"}

// shellPane is the pane in the caller's window titled Shell, else the other
// one. By window id, for the reason pr-pane gives.
func shellPane() (id, cmd string, err error) {
	me := os.Getenv("TMUX_PANE")
	panes, err := windowPanes(me)
	if err != nil {
		return "", "", err
	}
	for _, p := range panes {
		if p.title == "Shell" && p.id != me {
			return p.id, p.cmd, nil
		}
	}
	for _, p := range panes {
		if p.id != me {
			return p.id, p.cmd, nil
		}
	}
	return "", "", errors.New("no second pane in this window")
}

// agentPane is where the agent runs, from the code pane's point of view: the
// pane titled Agent, else one that is not a shell, else any other.
func agentPane(me string) (string, error) {
	panes, err := windowPanes(me)
	if err != nil {
		return "", err
	}
	others := slices.DeleteFunc(panes, func(p tpane) bool { return p.id == me })
	if len(others) == 0 {
		return "", errors.New("no agent pane in this window")
	}
	for _, p := range others {
		if p.title == "Agent" {
			return p.id, nil
		}
	}
	for _, p := range others {
		if !slices.Contains(shells, p.cmd) {
			return p.id, nil
		}
	}
	return others[0].id, nil
}

type tpane struct{ id, title, cmd string }

func windowPanes(me string) ([]tpane, error) {
	win, err := tmux("display-message", "-t", me, "-p", "#{window_id}")
	if err != nil {
		return nil, fmt.Errorf("tmux: %v", err)
	}
	out, err := tmux("list-panes", "-t", strings.TrimSpace(win), "-F", "#{pane_id}\t#{pane_title}\t#{pane_current_command}")
	if err != nil {
		return nil, fmt.Errorf("tmux: %v", err)
	}
	var ps []tpane
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.SplitN(l, "\t", 3)
		if len(f) == 3 {
			ps = append(ps, tpane{f[0], f[1], f[2]})
		}
	}
	return ps, nil
}

// askAgent pastes text into the agent's input and submits it. Bracketed paste
// keeps a multi-line message one message.
func askAgent(me, text string) error {
	target, err := agentPane(me)
	if err != nil {
		return err
	}
	load := exec.Command("tmux", "load-buffer", "-b", "prboom-ask", "-")
	load.Stdin = strings.NewReader(text)
	if out, err := load.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux: %s", strings.TrimSpace(string(out)))
	}
	if _, err := tmux("paste-buffer", "-p", "-d", "-b", "prboom-ask", "-t", target); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := tmux("send-keys", "-t", target, "Enter"); err != nil {
		return err
	}
	_, _ = tmux("select-pane", "-t", target)
	return nil
}

func tmux(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	if ee, ok := err.(*exec.ExitError); ok {
		return "", errors.New(strings.TrimSpace(string(ee.Stderr)))
	}
	return string(out), err
}

func viewerAlive(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "view.pid"))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return err == nil && pid > 0 && syscall.Kill(pid, 0) == nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

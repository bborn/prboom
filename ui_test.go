package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func fakePRs(n int) []PR {
	prs := make([]PR, n)
	for i := range prs {
		p := &prs[i]
		p.Number = 3500 + i
		p.Title = strings.Repeat(fmt.Sprintf("change number %d with a title that runs long ", i), 3)
		p.Author.Login = []string{"sgartland4304", "bborn", "someone-with-a-very-long-login"}[i%3]
		p.Additions, p.Deletions, p.ChangedFiles = i*37, i*3, i%9+1
		p.UpdatedAt = time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339)
		p.HeadRefName, p.BaseRefName = fmt.Sprintf("branch-%d", i), "main"
		p.Checks = []string{"green", "red", "pending", "none"}[i%4]
		p.IsDraft = i%5 == 4
	}
	return prs
}

func update(m model, msgs ...tea.Msg) model {
	var tm tea.Model = m
	for _, msg := range msgs {
		tm, _ = tm.Update(msg)
	}
	return tm.(model)
}

func keys(m model, ks ...string) model {
	special := map[string]tea.KeyType{
		"tab": tea.KeyTab, "esc": tea.KeyEsc, "enter": tea.KeyEnter, "down": tea.KeyDown, "up": tea.KeyUp,
	}
	for _, k := range ks {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		if t, ok := special[k]; ok {
			msg = tea.KeyMsg{Type: t}
		}
		m = update(m, msg)
	}
	return m
}

func loaded(w, h int, repo string, prs []PR) model {
	m := newModel(options{repo: repo, scope: "all", sortBy: "recency", limit: 60, dark: true})
	return update(m, tea.WindowSizeMsg{Width: w, Height: h}, loadedMsg{scope: 1, prs: prs, at: time.Now()})
}

// prboom 1234 and ⏎ on it run the same thing, as do prboom 1234 -ty and t.
func TestCommandForAPR(t *testing.T) {
	o := options{repo: "o/r", agent: "codex"}
	for _, c := range []struct {
		kind    string
		reviews map[int]Review
		want    string
	}{
		{"open", nil, "pr-open --agent codex 1234 -R o/r"},
		{"task", nil, "pr-task --agent codex 1234 -R o/r"},
		{"task", map[int]Review{1234: {TaskID: 5436}}, "ty open 5436"},
		{"close", nil, "pr-close 1234"},
	} {
		name, args := command(c.kind, 1234, o, c.reviews)
		if got := strings.Join(append([]string{name}, args...), " "); got != c.want {
			t.Errorf("%s: got %q, want %q", c.kind, got, c.want)
		}
	}
}

// L runs the learn script with the repo and agent as its arguments, whatever PR
// the cursor is on.
func TestCommandForLearn(t *testing.T) {
	name, args := command("learn", 1234, options{repo: "o/r", agent: "codex"}, nil)
	if name != "sh" || len(args) != 5 || args[1] != learnScript || args[3] != "o/r" || args[4] != "codex" {
		t.Errorf("got %s %q", name, args)
	}
}

// Opening prboom again in a repo lands on the list, sort and narrowing it was
// left with, while a flag given this time still wins.
func TestViewStateComesBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	was := loaded(120, 40, "o/r", fakePRs(9))
	was = keys(was, "s", "s", "f", "/", "c", "h", "enter")
	writeState(path, was.state())

	s, ok := readState(path)
	if !ok {
		t.Fatal("saved state did not read back")
	}
	o := options{repo: "o/r", scope: "review", sortBy: "recency", limit: 60, dark: true}
	s.apply(&o, map[string]bool{})
	now := update(newModel(o), tea.WindowSizeMsg{Width: 120, Height: 40},
		loadedMsg{scope: 1, prs: fakePRs(9), at: time.Now()})
	if now.state() != was.state() {
		t.Fatalf("reopened as %+v, left as %+v", now.state(), was.state())
	}
	if len(now.prs) == 0 || len(now.prs) != len(was.prs) {
		t.Fatalf("reopened showing %d PRs, left showing %d", len(now.prs), len(was.prs))
	}

	o = options{scope: "all", sortBy: "files"}
	s.apply(&o, map[string]bool{"s": true})
	if o.sortBy != "files" {
		t.Fatalf("-s files was overridden by the saved sort %q", o.sortBy)
	}
}

// The old picker printed rows wider than the terminal and more of them than
// fit, so the terminal wrapped and scrolled and the cursor row went off screen.
func TestViewFitsTheTerminalExactly(t *testing.T) {
	body := "## Why\n\n\tindented with a tab\n\n" + strings.Repeat("a very long line of description ", 20) +
		"\n\n```go\nfunc main() {}\n```\n"
	d := Detail{Body: body, Mergeable: "CONFLICTING"}
	d.Files = append(d.Files, struct {
		Path      string `json:"path"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
	}{"app/models/a/deeply/nested/path/that/goes/on/offer.rb", 120, 4})

	for _, size := range [][2]int{{40, 8}, {60, 12}, {80, 24}, {100, 30}, {120, 40}, {160, 50}, {220, 60}} {
		w, h := size[0], size[1]
		m := loaded(w, h, "", fakePRs(40))
		m = update(m, detailMsg{n: m.prs[0].Number, d: d},
			diffMsg{n: m.prs[0].Number, width: 10, raw: "+x", out: strings.Repeat("+wide diff line ", 30)})
		for tab := range tabs {
			for _, view := range []string{"list", "help", "filter"} {
				v := m
				switch view {
				case "help":
					v = keys(v, "?")
				case "filter":
					v = keys(v, "/", "c", "h")
				}
				lines := strings.Split(v.View(), "\n")
				if len(lines) != h {
					t.Errorf("%dx%d tab %d %s: %d lines, want %d", w, h, tab, view, len(lines), h)
				}
				for i, l := range lines {
					if lw := lipgloss.Width(l); lw > w {
						t.Errorf("%dx%d tab %d %s: line %d is %d wide: %q", w, h, tab, view, i, lw, l)
					}
				}
			}
			m = keys(m, "tab")
		}
	}
}

func TestCursorStaysOnScreen(t *testing.T) {
	m := loaded(80, 16, "", fakePRs(50))
	for i := 0; i < 45; i++ {
		m = keys(m, "j")
		cur, _ := m.current()
		if !strings.Contains(m.View(), fmt.Sprintf("#%d", cur.Number)) {
			t.Fatalf("after %d moves #%d is not on screen:\n%s", i+1, cur.Number, m.View())
		}
	}
	m = keys(m, "g")
	if m.idx != 0 || !strings.Contains(m.View(), "#3500") {
		t.Fatalf("g did not return to the top")
	}
}

func TestFilterNarrowsAndClears(t *testing.T) {
	m := loaded(120, 30, "", fakePRs(30))
	m = keys(m, "/", "3", "5", "1", "7", "enter")
	if len(m.prs) != 1 || m.prs[0].Number != 3517 {
		t.Fatalf("filter #3517 left %d PRs", len(m.prs))
	}
	if !strings.Contains(m.View(), "/3517") {
		t.Errorf("header does not show the filter")
	}
	m = keys(m, "esc")
	if len(m.prs) != 30 || m.prs[m.idx].Number != 3517 {
		t.Fatalf("esc should clear the filter and keep the cursor on #3517, got %d PRs at #%d",
			len(m.prs), m.prs[m.idx].Number)
	}
}

// Found by driving it in tmux: keys sent together arrive as one message, and
// the picker matched none of "jj", "JJJJ" or "/ship".
func TestKeysThatArriveTogether(t *testing.T) {
	m := loaded(120, 30, "", fakePRs(30))
	m = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjj")})
	if m.idx != 3 {
		t.Fatalf("jjj moved to %d, want 3", m.idx)
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/3517")}, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.prs) != 1 || m.prs[0].Number != 3517 {
		t.Fatalf("/3517 in one message left %d PRs", len(m.prs))
	}
}

// pr-open's complaint used to vanish the instant the list redrew.
func TestHoldOnFailureWaitsAndKeepsTheStatus(t *testing.T) {
	cmd := exec.Command("sh", "-c", holdOnFailure, "sh", "-c", "echo could not fetch; exit 3")
	cmd.Stdin = strings.NewReader("\n")
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 3 {
		t.Fatalf("want exit 3, got %v", err)
	}
	if !strings.Contains(string(out), "could not fetch") || !strings.Contains(string(out), "enter goes back") {
		t.Fatalf("failure not held with its message: %q", out)
	}

	out, err = exec.Command("sh", "-c", holdOnFailure, "sh", "-c", "exit 0").CombinedOutput()
	if err != nil || strings.Contains(string(out), "enter goes back") {
		t.Fatalf("success should pass straight through: %v %q", err, out)
	}
}

func TestAuthorToggleAndSort(t *testing.T) {
	m := loaded(120, 30, "", fakePRs(30))
	m = keys(m, "f")
	for _, p := range m.prs {
		if p.Author.Login != "sgartland4304" {
			t.Fatalf("f let through %s", p.Author.Login)
		}
	}
	m = keys(m, "f", "s", "s")
	if m.opt.sortBy != "loc" || m.prs[0].Number != 3529 {
		t.Fatalf("sort by loc: got %s with #%d first", m.opt.sortBy, m.prs[0].Number)
	}
}

// A checkout whose origin has no PRs used to say "nothing waiting on you"
// even for every open PR, with no hint of which repo it had asked.
func TestEmptyListNamesTheRepo(t *testing.T) {
	m := loaded(100, 20, "bborn/offerlab-gm", nil)
	if v := m.View(); !strings.Contains(v, "Nothing in all open in bborn/offerlab-gm.") {
		t.Fatalf("empty view does not name the repo:\n%s", v)
	}
}

func TestSwitchingScopeShowsTheCacheWhileItRefreshes(t *testing.T) {
	m := loaded(120, 30, "", fakePRs(10))
	m = update(m, cachedMsg{scope: 0, prs: fakePRs(3), at: time.Now().Add(-time.Hour)})
	m = keys(m, "1")
	if len(m.prs) != 3 || !m.loading[0] {
		t.Fatalf("scope 1: %d PRs, loading %v", len(m.prs), m.loading[0])
	}
	if !strings.Contains(m.View(), "refreshing") {
		t.Errorf("header does not say it is refreshing")
	}

	m = update(m, loadedMsg{scope: 0, prs: fakePRs(5), at: time.Now()},
		cachedMsg{scope: 0, prs: fakePRs(1), at: time.Now()})
	if len(m.prs) != 5 {
		t.Fatalf("a late cache read replaced the fetched list: %d PRs", len(m.prs))
	}
}

func TestDetailFetchWaitsForTheCursorToRest(t *testing.T) {
	m := loaded(120, 30, "", fakePRs(10))
	if !m.inflight[detailKey(m.prs[0].Number)] {
		t.Fatalf("the first PR should be fetched as soon as the list lands: %v", m.inflight)
	}
	m = update(m, detailMsg{n: m.prs[0].Number})
	m = keys(m, "j", "j", "j")
	if m.settled || len(m.inflight) != 0 {
		t.Fatalf("fetching while the cursor is moving: %v", m.inflight)
	}
	m = update(m, settledMsg(m.seq))
	if !m.inflight[detailKey(m.prs[3].Number)] || len(m.inflight) != 1 {
		t.Fatalf("resting on #%d should fetch it and only it: %v", m.prs[3].Number, m.inflight)
	}
}

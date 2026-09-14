package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	dim    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "244", Dark: "240"})
	bold   = lipgloss.NewStyle().Bold(true)
	cursor = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "33", Dark: "39"}).Bold(true)
	errSty = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"})
	red    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"})
	green  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "78"})
	amber  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "214"})
)

// Action is what the caller should run after the list exits.
type Action struct {
	Kind   string // "open", "task", "diff", ""
	PR     PR
	Review Review // what is already under way for PR, if anything
}

type loadedMsg struct {
	prs     []PR
	reviews map[int]Review
	err     error
}

type model struct {
	repo  string
	mine  bool
	limit int
	width int

	prs     []PR
	reviews map[int]Review
	idx     int
	loading bool
	err     error

	action Action
}

func newModel(repo string, mine bool, limit int) model {
	return model{repo: repo, mine: mine, limit: limit, loading: true, width: 100}
}

func (m model) load() tea.Cmd {
	repo, mine, limit := m.repo, m.mine, m.limit
	return func() tea.Msg {
		reviews := make(chan map[int]Review, 1)
		go func() { reviews <- reviewsFor(repo) }()
		prs, err := Fetch(repo, mine, limit)
		return loadedMsg{prs: prs, reviews: <-reviews, err: err}
	}
}

// reviewsFor only looks locally when the list is for the repo you are in;
// with -R the local worktrees and tasks belong to some other repo.
func reviewsFor(repo string) map[int]Review {
	if repo != "" {
		return map[int]Review{}
	}
	return Reviews()
}

func (m model) Init() tea.Cmd { return m.load() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width

	case loadedMsg:
		m.loading = false
		m.prs, m.reviews, m.err = msg.prs, msg.reviews, msg.err
		if m.idx >= len(m.prs) {
			m.idx = 0
		}

	case tea.KeyMsg:
		if m.loading {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			if m.idx > 0 {
				m.idx--
			}
		case "down", "j":
			if m.idx < len(m.prs)-1 {
				m.idx++
			}
		case "g", "home":
			m.idx = 0
		case "G", "end":
			m.idx = max(0, len(m.prs)-1)

		case "enter":
			if cur, ok := m.current(); ok {
				m.action = Action{Kind: "open", PR: cur}
				return m, tea.Quit
			}
		case "t":
			if cur, ok := m.current(); ok {
				m.action = Action{Kind: "task", PR: cur, Review: m.reviews[cur.Number]}
				return m, tea.Quit
			}
		case "d":
			if cur, ok := m.current(); ok {
				m.action = Action{Kind: "diff", PR: cur}
				return m, tea.Quit
			}
		case "o":
			if cur, ok := m.current(); ok {
				openBrowser(cur.URL)
			}

		case "a":
			m.mine = !m.mine
			m.loading = true
			return m, m.load()
		case "r":
			m.loading = true
			return m, m.load()
		}
	}
	return m, nil
}

func (m model) current() (PR, bool) {
	if m.idx < 0 || m.idx >= len(m.prs) {
		return PR{}, false
	}
	return m.prs[m.idx], true
}

func checkMark(state string) string {
	switch state {
	case "red":
		return red.Render("✗")
	case "green":
		return green.Render("✓")
	case "pending":
		return amber.Render("•")
	}
	return " "
}

func (m model) View() string {
	scope := "review-requested"
	if !m.mine {
		scope = "all open"
	}

	if m.loading {
		return "\n  " + dim.Render("reading "+scope+" pull requests…") + "\n\n"
	}
	if m.err != nil {
		return "\n  " + errSty.Render(m.err.Error()) + "\n\n  " + dim.Render("r retry   q quit") + "\n\n"
	}
	if len(m.prs) == 0 {
		return "\n  " + dim.Render("nothing in "+scope+".") + "\n\n  " +
			dim.Render("a all open   r refresh   q quit") + "\n\n"
	}

	titleWidth := m.width - 57
	if titleWidth < 20 {
		titleWidth = 20
	}

	var b strings.Builder
	b.WriteString("\n")
	for i, p := range m.prs {
		mark := "  "
		style := lipgloss.NewStyle()
		if i == m.idx {
			mark = cursor.Render("▸ ")
			style = bold
		}
		reviewing := strings.Repeat(" ", 8)
		if r, ok := m.reviews[p.Number]; ok {
			reviewing = cursor.Render(pad(r.Label(), 8))
		}
		b.WriteString(fmt.Sprintf("%s%s %s %s %s %s %s\n",
			mark,
			checkMark(p.Checks),
			style.Render(pad("#"+fmt.Sprint(p.Number), 6)),
			reviewing,
			dim.Render(pad(fmt.Sprintf("+%d/-%d", p.Additions, p.Deletions), 13)),
			dim.Render(pad(fmt.Sprintf("%df", p.ChangedFiles), 5)),
			style.Render(truncate(p.Title, titleWidth)),
		))
	}

	if cur, ok := m.current(); ok {
		detail := fmt.Sprintf("%s · %dd ago · %s → %s",
			cur.Author.Login, cur.AgeDays, cur.HeadRefName, cur.BaseRefName)
		if cur.IsCrossRepository {
			detail += " · fork"
		}
		if cur.IsDraft {
			detail += " · draft"
		}
		b.WriteString("\n  " + dim.Render(detail) + "\n")
		if r, ok := m.reviews[cur.Number]; ok {
			b.WriteString("  " + cursor.Render(r.Detail()) + "\n")
		}
	}

	b.WriteString("\n  " + dim.Render("↑↓ move   ⏎ walk it   t as a ty task   d diff   o browser   a "+
		map[bool]string{true: "all open", false: "just mine"}[m.mine]+"   r refresh   q quit") + "\n\n")
	return b.String()
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

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

	verdictStyle = map[string]lipgloss.Style{
		VerdictStandard:   lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "78"}),
		VerdictMechanical: lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "30", Dark: "80"}),
		VerdictHeavy:      lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "214"}),
		VerdictNeedsPlan:  lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "90", Dark: "177"}),
		VerdictBlocked:    lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"}),
	}
)

// Action is what the caller should run after the list exits.
type Action struct {
	Kind string // "start", "diff", ""
	PR   PR
}

type loadedMsg struct {
	prs []PR
	err error
}

type model struct {
	repo  string
	mine  bool
	limit int
	width int

	prs     []PR
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
		prs, err := Fetch(repo, mine, limit)
		return loadedMsg{prs: prs, err: err}
	}
}

func (m model) Init() tea.Cmd { return m.load() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width

	case loadedMsg:
		m.loading = false
		m.prs, m.err = msg.prs, msg.err
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
				m.action = Action{Kind: "start", PR: cur}
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

	// Room for: cursor, verdict, number, size, files, age, author, then title.
	titleWidth := m.width - 62
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
		size := fmt.Sprintf("+%d/-%d", p.Additions, p.Deletions)
		b.WriteString(fmt.Sprintf("%s%s %s %s %s %s %s\n",
			mark,
			verdictStyle[p.Verdict].Render(pad(p.Verdict, 10)),
			style.Render(pad("#"+fmt.Sprint(p.Number), 6)),
			dim.Render(pad(size, 12)),
			dim.Render(pad(fmt.Sprintf("%df", p.ChangedFiles), 4)),
			dim.Render(pad(fmt.Sprintf("%dd", p.AgeDays), 4)),
			style.Render(truncate(p.Title, titleWidth)),
		))
	}

	if cur, ok := m.current(); ok {
		detail := fmt.Sprintf("%s · checks %s · %s → %s",
			cur.Author.Login, cur.Checks, cur.HeadRefName, cur.BaseRefName)
		if cur.Why != "" {
			detail = cur.Why + " · " + detail
		}
		b.WriteString("\n  " + dim.Render(detail) + "\n")
	}

	b.WriteString("\n  " + dim.Render("↑↓ move   ⏎ check out   d diff   o browser   a "+
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

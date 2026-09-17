package main

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	dim    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "244", Dark: "244"})
	faint  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "251", Dark: "238"})
	bold   = lipgloss.NewStyle().Bold(true)
	accent = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "33", Dark: "39"})
	strong = accent.Bold(true)
	errSty = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"})
	red    = errSty
	green  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "78"})
	amber  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "214"})
	selBg  = lipgloss.AdaptiveColor{Light: "254", Dark: "236"}
)

// The detail pane's views, in the order tab cycles through them.
var tabs = []string{"Overview", "Files", "Diff"}

const (
	detailHead   = 5                      // title, meta, status, tabs, rule
	settleAfter  = 120 * time.Millisecond // the cursor rests this long before its PR is fetched
	staleAfter   = 30 * time.Second       // switching to a list older than this refetches it
	refreshAfter = 3 * time.Minute        // left open, the current list refreshes itself
)

type options struct {
	repo, scope, author, sortBy, agent string
	only, query                        string // narrowing restored from last time
	limit                              int
	dark                               bool
}

type (
	loadedMsg struct {
		scope   int
		prs     []PR
		reviews map[int]Review
		err     error
		at      time.Time
	}
	cachedMsg struct {
		scope int
		prs   []PR
		at    time.Time
	}
	detailMsg struct {
		n   int
		d   Detail
		err error
	}
	diffMsg struct {
		n, width int
		raw, out string
		err      error
	}
	execDoneMsg struct {
		what string
		err  error
	}
	repoMsg      string
	reviewsMsg   map[int]Review
	settledMsg   int
	flashDoneMsg int
	refreshMsg   struct{}
)

type rendered struct {
	width int
	out   string
}

type model struct {
	opt      options
	repoName string

	// One entry per scope.
	scope   int
	lists   [][]PR
	have    []bool // fetched this run, not just read from the cache
	at      []time.Time
	loading []bool
	errs    []error

	all       []PR // the current scope's list
	prs       []PR // what is shown: all, filtered and sorted
	reviews   map[int]Review
	idx       int
	offset    int
	only      string // f: one author, no refetch
	query     textinput.Model
	filtering bool

	width, height int
	preview       bool
	help          bool
	tab           int
	vp            viewport.Model
	vpSig         string
	vpAt          string // PR and tab the viewport's scroll position belongs to

	details    map[int]Detail
	detailErrs map[int]error
	diffs      map[int]string
	renders    map[int]rendered
	diffErrs   map[int]error
	inflight   map[string]bool
	bodies     map[string]string
	md         *glamour.TermRenderer
	mdWidth    int

	seq     int
	settled bool

	spin     spinner.Model
	flash    string
	flashErr bool
	flashSeq int
}

func newModel(o options) model {
	q := textinput.New()
	q.Prompt = "/ "
	q.Placeholder = "title, author, branch or #number"
	q.Cursor.SetMode(cursor.CursorStatic)
	q.SetValue(o.query)

	scope := 0
	for i, s := range scopes {
		if s.Key == o.scope {
			scope = i
		}
	}
	n := len(scopes)
	m := model{
		opt:        o,
		repoName:   o.repo,
		scope:      scope,
		only:       o.only,
		lists:      make([][]PR, n),
		have:       make([]bool, n),
		at:         make([]time.Time, n),
		loading:    make([]bool, n),
		errs:       make([]error, n),
		reviews:    map[int]Review{},
		query:      q,
		preview:    true,
		vp:         viewport.New(0, 0),
		details:    map[int]Detail{},
		detailErrs: map[int]error{},
		diffs:      map[int]string{},
		renders:    map[int]rendered{},
		diffErrs:   map[int]error{},
		inflight:   map[string]bool{},
		bodies:     map[string]string{},
		spin:       spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(accent)),
	}
	m.loading[scope] = true
	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, m.fetch(m.scope), refreshLater()}
	for i := range scopes {
		cmds = append(cmds, m.cached(i))
	}
	if m.opt.repo == "" {
		cmds = append(cmds, repoName)
	}
	return tea.Batch(cmds...)
}

func (m model) fetch(i int) tea.Cmd {
	o := m.opt
	return func() tea.Msg {
		reviews := make(chan map[int]Review, 1)
		go func() { reviews <- reviewsFor(o.repo) }()
		prs, err := Fetch(o.repo, scopes[i].Key, o.author, o.limit)
		if err == nil {
			writeCache(cacheFile(o.repo, scopes[i].Key, o.author, o.limit), prs)
		}
		return loadedMsg{scope: i, prs: prs, reviews: <-reviews, err: err, at: time.Now()}
	}
}

func (m model) cached(i int) tea.Cmd {
	o := m.opt
	return func() tea.Msg {
		prs, at, ok := readCache(cacheFile(o.repo, scopes[i].Key, o.author, o.limit))
		if !ok {
			return nil
		}
		return cachedMsg{scope: i, prs: prs, at: at}
	}
}

func repoName() tea.Msg {
	out, err := gh("repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	if err != nil {
		return nil
	}
	return repoMsg(strings.TrimSpace(string(out)))
}

func refreshLater() tea.Cmd {
	return tea.Tick(refreshAfter, func(time.Time) tea.Msg { return refreshMsg{} })
}

// reviewsFor only looks locally when the list is for the repo you are in;
// with -R the local worktrees and tasks belong to some other repo.
func reviewsFor(repo string) map[int]Review {
	if repo != "" {
		return map[int]Review{}
	}
	return Reviews()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// A resize re-renders the diff, so wait for the drag to stop.
		cmds = append(cmds, m.touch())

	case spinner.TickMsg:
		var c tea.Cmd
		m.spin, c = m.spin.Update(msg)
		cmds = append(cmds, c)

	case cachedMsg:
		// Only until the real list lands; after that the cache is older news.
		if !m.have[msg.scope] {
			m.lists[msg.scope], m.at[msg.scope] = msg.prs, msg.at
			if msg.scope == m.scope {
				m.all = msg.prs
				m.filter()
				m.settled = true
			}
		}

	case loadedMsg:
		m.loading[msg.scope] = false
		m.errs[msg.scope] = msg.err
		if msg.err == nil {
			m.lists[msg.scope], m.at[msg.scope], m.have[msg.scope] = msg.prs, msg.at, true
		}
		if msg.reviews != nil {
			m.reviews = msg.reviews
		}
		if msg.scope == m.scope {
			m.all = m.lists[m.scope]
			m.filter()
			m.settled = true
		}

	case repoMsg:
		m.repoName = string(msg)

	case reviewsMsg:
		m.reviews = msg

	case detailMsg:
		delete(m.inflight, detailKey(msg.n))
		if msg.err != nil {
			m.detailErrs[msg.n] = msg.err
		} else {
			m.details[msg.n] = msg.d
		}

	case diffMsg:
		delete(m.inflight, diffKey(msg.n, msg.width))
		if msg.err != nil {
			m.diffErrs[msg.n] = msg.err
		} else {
			m.diffs[msg.n] = msg.raw
			m.renders[msg.n] = rendered{msg.width, msg.out}
		}

	case settledMsg:
		if int(msg) == m.seq {
			m.settled = true
		}

	case refreshMsg:
		cmds = append(cmds, m.startFetch(m.scope), refreshLater())

	case execDoneMsg:
		if msg.err != nil {
			cmds = append(cmds, m.say(msg.what+": "+msg.err.Error(), true))
		}
		// Whatever ran may have opened a worktree or a task.
		repo := m.opt.repo
		cmds = append(cmds, func() tea.Msg { return reviewsMsg(reviewsFor(repo)) })

	case flashDoneMsg:
		if int(msg) == m.flashSeq {
			m.flash = ""
		}

	case tea.KeyMsg:
		cmds = append(cmds, m.key(msg))
	}

	cmds = append(cmds, m.sync()...)
	return m, tea.Batch(cmds...)
}

func (m *model) key(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		return tea.Quit
	}

	// Keys that arrive together come as one message: held j is "jjj", and
	// /ship typed quickly is "/ship". Take them one at a time, so each lands
	// where it would have alone, the filter included.
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste && !m.filtering {
		var cmds []tea.Cmd
		for _, r := range msg.Runes {
			cmds = append(cmds, m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: msg.Alt}))
		}
		return tea.Batch(cmds...)
	}

	if m.filtering {
		switch k {
		case "esc":
			m.query.SetValue("")
			fallthrough
		case "enter":
			m.filtering = false
			m.query.Blur()
			m.filter()
			return m.touch()
		case "up", "ctrl+p":
			return m.move(-1)
		case "down", "ctrl+n":
			return m.move(1)
		}
		var c tea.Cmd
		m.query, c = m.query.Update(msg)
		m.filter()
		return tea.Batch(c, m.touch())
	}

	if m.help {
		m.help = false
		return nil
	}

	cur, ok := m.current()
	switch k {
	case "q":
		return tea.Quit
	case "esc":
		// Peel back one narrowing at a time before leaving.
		switch {
		case m.query.Value() != "":
			m.query.SetValue("")
		case m.only != "":
			m.only = ""
		default:
			return tea.Quit
		}
		m.filter()
		return m.touch()
	case "?":
		m.help = true

	case "up", "k":
		return m.move(-1)
	case "down", "j":
		return m.move(1)
	case "pgup":
		return m.move(-m.layout().listH)
	case "pgdown":
		return m.move(m.layout().listH)
	case "g", "home":
		return m.move(-len(m.prs))
	case "G", "end":
		return m.move(len(m.prs))

	case "J":
		m.vp.ScrollDown(1)
	case "K":
		m.vp.ScrollUp(1)
	case "ctrl+d", " ":
		m.vp.HalfPageDown()
	case "ctrl+u":
		m.vp.HalfPageUp()
	case "tab":
		m.tab = (m.tab + 1) % len(tabs)
	case "shift+tab":
		m.tab = (m.tab + len(tabs) - 1) % len(tabs)
	case "p":
		m.preview = !m.preview

	case "1", "2", "3":
		return m.setScope(int(k[0] - '1'))
	case "a":
		if m.scope == 1 {
			return m.setScope(0)
		}
		return m.setScope(1)
	case "r":
		if ok {
			n := cur.Number
			delete(m.details, n)
			delete(m.detailErrs, n)
			delete(m.diffs, n)
			delete(m.renders, n)
			delete(m.diffErrs, n)
			clear(m.bodies)
		}
		return m.startFetch(m.scope)

	case "/":
		m.filtering = true
		return m.query.Focus()
	case "f":
		if m.only != "" {
			m.only = ""
		} else if ok {
			m.only = cur.Author.Login
		}
		m.filter()
		return m.touch()
	case "s":
		m.opt.sortBy = sorts[(slices.Index(sorts, m.opt.sortBy)+1)%len(sorts)]
		m.filter()
		return m.touch()

	case "L":
		return m.launch("learn", cur)
	case "enter":
		if ok {
			return m.launch("open", cur)
		}
	case "t":
		if ok {
			return m.launch("task", cur)
		}
	case "d":
		if ok {
			return m.launch("diff", cur)
		}
	case "x":
		if ok {
			if r := m.reviews[cur.Number]; r.TaskID != 0 && !r.Local {
				return m.say(fmt.Sprintf("#%d is TaskYou's: close task %d in ty", cur.Number, r.TaskID), true)
			} else if !r.Local {
				return m.say(fmt.Sprintf("nothing open for #%d", cur.Number), false)
			}
			return m.launch("close", cur)
		}
	case "o":
		if ok {
			openBrowser(cur.URL)
			return m.say(fmt.Sprintf("opened #%d in the browser", cur.Number), false)
		}
	case "y":
		if ok {
			c := exec.Command("pbcopy")
			c.Stdin = strings.NewReader(cur.URL)
			if err := c.Run(); err != nil {
				return m.say("could not copy: "+err.Error(), true)
			}
			return m.say("copied "+cur.URL, false)
		}
	}
	return nil
}

// diffScript pages a PR's whole diff. $1 is the number, $2 the repo or empty.
const diffScript = `gh pr diff "$1" ${2:+-R "$2"} | delta --paging=always --navigate --line-numbers ` +
	`--side-by-side --hyperlinks --hyperlinks-file-link-format "file://{path}#{line}"`

// learnScript starts the agent on the pr-learn skill for the repo, in this
// terminal: it reads PRs and writes rules, so it needs no worktree. $1 is the
// repo and $2 the --agent, either empty. Claude gets the slash command; any
// other agent the file.
const learnScript = `. "$(dirname "$(readlink -f "$(command -v pr-learn)")")/prboom-env"
[ -n "$2" ] && PR_AGENT=$2
args=${1:+-R $1}
case "$PR_AGENT" in
  claude) exec claude "/pr-learn $args" ;;
  *) exec "$PR_AGENT" "Read $PRBOOM_HOME/skills/pr-learn/SKILL.md and follow it exactly. Arguments: $args" ;;
esac`

// holdOnFailure runs "$0" "$@" and, only if it fails, waits for enter.
const holdOnFailure = `"$0" "$@" || { s=$?; ` +
	`printf '\n\033[2mexited %s · enter goes back to the list\033[0m' "$s"; read -r _; exit $s; }`

// command is what a kind of launch runs for PR n: "open" walks it, "task" walks
// it as a TaskYou task, "diff" pages its diff, "close" throws a walk away.
// "learn" ignores n: it teaches the walk how you review this repo.
func command(kind string, n int, o options, reviews map[int]Review) (string, []string) {
	var args []string
	if o.agent != "" {
		args = append(args, "--agent", o.agent)
	}
	args = append(args, fmt.Sprint(n))
	if o.repo != "" {
		args = append(args, "-R", o.repo)
	}

	switch kind {
	case "task":
		// Already on the board: go back to that task rather than make a twin.
		if id := reviews[n].TaskID; id != 0 {
			return "ty", []string{"open", fmt.Sprint(id)}
		}
		return "pr-task", args
	case "diff":
		return "sh", []string{"-c", diffScript, "prboom-diff", fmt.Sprint(n), o.repo}
	case "close":
		// By number, in the repo you are in, which is the only place pr-open
		// put anything. A worktree with uncommitted work is kept and says so.
		return "pr-close", []string{fmt.Sprint(n)}
	case "learn":
		return "sh", []string{"-c", learnScript, "prboom-learn", o.repo, o.agent}
	}
	// git + tmux only. Nothing else is required to walk a PR.
	return "pr-open", args
}

// launch hands the terminal to another command and takes it back after, so a
// walk, a task or a diff returns to the list instead of ending it.
func (m *model) launch(kind string, p PR) tea.Cmd {
	name, args := command(kind, p.Number, m.opt, m.reviews)
	path, err := exec.LookPath(name)
	if err != nil {
		return m.say(name+" is not on PATH", true)
	}
	// The list redraws the instant the command exits, which would wipe a
	// failure's explanation before anyone read it.
	held := append([]string{"-c", holdOnFailure, path}, args...)
	return tea.ExecProcess(exec.Command("sh", held...), func(err error) tea.Msg {
		return execDoneMsg{what: name, err: err}
	})
}

func (m *model) say(s string, isErr bool) tea.Cmd {
	m.flash, m.flashErr = s, isErr
	m.flashSeq++
	seq := m.flashSeq
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return flashDoneMsg(seq) })
}

func (m *model) startFetch(i int) tea.Cmd {
	if m.loading[i] {
		return nil
	}
	m.loading[i] = true
	return m.fetch(i)
}

func (m *model) setScope(i int) tea.Cmd {
	m.scope = i
	m.all = m.lists[i]
	m.filter()
	var fetch tea.Cmd
	if !m.have[i] || time.Since(m.at[i]) > staleAfter {
		fetch = m.startFetch(i)
	}
	return tea.Batch(fetch, m.touch())
}

func (m *model) move(d int) tea.Cmd {
	if len(m.prs) == 0 {
		return nil
	}
	i := min(max(m.idx+d, 0), len(m.prs)-1)
	if i == m.idx {
		return nil
	}
	m.idx = i
	return m.touch()
}

// touch marks the cursor as moving. Its PR is only fetched once it rests, so
// holding j does not start a gh call for every row it passes.
func (m *model) touch() tea.Cmd {
	m.seq++
	m.settled = false
	seq := m.seq
	return tea.Tick(settleAfter, func(time.Time) tea.Msg { return settledMsg(seq) })
}

// filter rebuilds the shown list from the scope's, keeping the cursor on the
// same PR when it survives.
func (m *model) filter() {
	cur, had := m.current()
	q := m.query.Value()
	m.prs = m.prs[:0:0]
	for _, p := range m.all {
		if (m.only == "" || p.Author.Login == m.only) && matches(p, q) {
			m.prs = append(m.prs, p)
		}
	}
	sortPRs(m.prs, m.opt.sortBy)
	m.idx = 0
	if had {
		for i, p := range m.prs {
			if p.Number == cur.Number {
				m.idx = i
				break
			}
		}
	}
}

func (m model) current() (PR, bool) {
	if m.idx < 0 || m.idx >= len(m.prs) {
		return PR{}, false
	}
	return m.prs[m.idx], true
}

type layout struct {
	listW, listH int
	detW, detH   int
	side         bool // detail beside the list rather than under it
	preview      bool
}

func (m model) layout() layout {
	bodyH := max(1, m.height-2)
	l := layout{listW: m.width, listH: bodyH}
	if !m.preview || len(m.prs) == 0 {
		return l
	}
	switch {
	case m.width >= 120:
		l.side, l.preview = true, true
		l.listW = max(64, m.width*11/20)
		l.detW, l.detH = m.width-l.listW-3, bodyH
	case bodyH >= 16:
		l.preview = true
		l.listH = min(max(4, bodyH*2/5), max(3, len(m.prs)))
		l.detW, l.detH = m.width, bodyH-l.listH-1
	}
	return l
}

// sync brings the scroll offset and the detail pane in line with the cursor,
// and asks for whatever the resting PR is missing.
func (m *model) sync() []tea.Cmd {
	if m.width == 0 {
		return nil
	}
	l := m.layout()
	m.offset = scrolled(m.offset, m.idx, len(m.prs), l.listH)
	if !l.preview {
		return nil
	}
	cur, ok := m.current()
	if !ok {
		return nil
	}

	var cmds []tea.Cmd
	if m.settled {
		cmds = m.want(cur, l.detW)
	}

	m.vp.Width, m.vp.Height = l.detW, max(0, l.detH-detailHead)
	if sig := m.signature(cur, l.detW); sig != m.vpSig {
		m.vp.SetContent(strings.ReplaceAll(m.content(cur, l.detW), "\t", "    "))
		m.vpSig = sig
	}
	if at := fmt.Sprintf("%d/%d", cur.Number, m.tab); at != m.vpAt {
		m.vp.GotoTop()
		m.vpAt = at
	}
	return cmds
}

// scrolled is the list offset that keeps idx on screen, moving as little as it can.
func scrolled(offset, idx, n, h int) int {
	if idx < offset {
		offset = idx
	}
	if idx >= offset+h {
		offset = idx - h + 1
	}
	return min(max(offset, 0), max(0, n-h))
}

func detailKey(n int) string      { return fmt.Sprintf("detail/%d", n) }
func diffKey(n, width int) string { return fmt.Sprintf("diff/%d/%d", n, width) }

func (m *model) want(p PR, w int) []tea.Cmd {
	var cmds []tea.Cmd
	n, o := p.Number, m.opt

	if _, ok := m.details[n]; !ok && m.detailErrs[n] == nil && !m.inflight[detailKey(n)] {
		m.inflight[detailKey(n)] = true
		cmds = append(cmds, func() tea.Msg {
			d, err := FetchDetail(o.repo, n)
			return detailMsg{n: n, d: d, err: err}
		})
	}

	if m.tab == 2 && m.diffErrs[n] == nil && m.renders[n].width != w && !m.inflight[diffKey(n, w)] {
		m.inflight[diffKey(n, w)] = true
		raw, fetched := m.diffs[n]
		cmds = append(cmds, func() tea.Msg {
			if !fetched {
				var err error
				if raw, err = FetchDiff(o.repo, n); err != nil {
					return diffMsg{n: n, width: w, err: err}
				}
			}
			return diffMsg{n: n, width: w, raw: raw, out: RenderDiff(raw, w, o.dark)}
		})
	}
	return cmds
}

// busy reports whether anything for PR n is still on its way.
func (m model) busy(n int) bool {
	if m.inflight[detailKey(n)] {
		return true
	}
	prefix := fmt.Sprintf("diff/%d/", n)
	for k := range m.inflight {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// signature changes exactly when the detail content would, so the viewport is
// not handed a fresh megabyte of diff on every spinner frame.
func (m model) signature(p PR, w int) string {
	n := p.Number
	state := "wait"
	if m.tab == 2 {
		switch {
		case m.renders[n].out != "":
			state = fmt.Sprintf("render%d", m.renders[n].width)
		case m.diffErrs[n] != nil:
			state = "err"
		}
	} else {
		switch _, ok := m.details[n]; {
		case ok:
			state = "ok"
		case m.detailErrs[n] != nil:
			state = "err"
		}
	}
	return fmt.Sprintf("%d/%d/%d/%s", n, m.tab, w, state)
}

func (m *model) content(p PR, w int) string {
	n := p.Number
	if m.tab == 2 {
		if r, ok := m.renders[n]; ok {
			return r.out
		}
		if err := m.diffErrs[n]; err != nil {
			return errSty.Render(err.Error()) + "\n\n" + dim.Render("d opens it in delta instead")
		}
		return dim.Render("reading the diff…")
	}

	d, ok := m.details[n]
	if !ok {
		if err := m.detailErrs[n]; err != nil {
			return errSty.Render(err.Error())
		}
		return dim.Render("reading #" + fmt.Sprint(n) + "…")
	}
	if m.tab == 1 {
		return files(p, d, w)
	}
	return m.overview(p, d, w)
}

func (m *model) overview(p PR, d Detail, w int) string {
	var b strings.Builder
	field := func(k, v string) {
		if v != "" {
			b.WriteString(dim.Render(pad(k, 11)) + v + "\n")
		}
	}

	var requested []string
	for _, r := range d.ReviewRequests {
		requested = append(requested, cmp(r.Login, r.Name))
	}
	field("Requested", strings.Join(requested, ", "))

	var reviews []string
	for _, r := range d.LatestReviews {
		reviews = append(reviews, r.Author.Login+" "+reviewState(r.State))
	}
	field("Reviews", strings.Join(reviews, dim.Render(" · ")))

	var labels []string
	for _, l := range d.Labels {
		labels = append(labels, amber.Render(l.Name))
	}
	field("Labels", strings.Join(labels, " "))

	field("Size", fmt.Sprintf("%s %s in %s · %s · %s",
		green.Render(fmt.Sprintf("+%d", p.Additions)), red.Render(fmt.Sprintf("-%d", p.Deletions)),
		plural(p.ChangedFiles, "file"), plural(len(d.Commits), "commit"), plural(len(d.Comments), "comment")))
	b.WriteString("\n")

	if body := strings.TrimSpace(d.Body); body == "" {
		b.WriteString("  " + dim.Render("No description."))
	} else {
		b.WriteString(m.markdown(p.Number, body, w))
	}
	return b.String()
}

func (m *model) markdown(n int, body string, w int) string {
	key := fmt.Sprintf("%d/%d", n, w)
	if s, ok := m.bodies[key]; ok {
		return s
	}
	if m.md == nil || m.mdWidth != w {
		style := "dark"
		if !m.opt.dark {
			style = "light"
		}
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(max(20, w-4)))
		if err != nil {
			return body
		}
		m.md, m.mdWidth = r, w
	}
	out, err := m.md.Render(body)
	if err != nil {
		return body
	}
	out = strings.Trim(out, "\n")
	m.bodies[key] = out
	return out
}

func files(p PR, d Detail, w int) string {
	if len(d.Files) == 0 {
		return "  " + dim.Render("No files.")
	}
	most := 1
	for _, f := range d.Files {
		most = max(most, f.Additions+f.Deletions)
	}
	numW := len(fmt.Sprint(most)) + 1
	const barW = 10
	pathW := max(8, w-2*numW-barW-6)

	var b strings.Builder
	for _, f := range d.Files {
		total := f.Additions + f.Deletions
		cells := 0
		if total > 0 {
			cells = max(1, (total*barW+most-1)/most)
		}
		plus := 0
		if total > 0 {
			plus = cells * f.Additions / total
		}
		bar := green.Render(strings.Repeat("■", plus)) + red.Render(strings.Repeat("■", cells-plus)) +
			strings.Repeat(" ", barW-cells)
		fmt.Fprintf(&b, " %s %s  %s  %s\n",
			green.Render(fmt.Sprintf("%*s", numW, fmt.Sprintf("+%d", f.Additions))),
			red.Render(fmt.Sprintf("%*s", numW, fmt.Sprintf("-%d", f.Deletions))),
			bar, truncLeft(f.Path, pathW))
	}
	if len(d.Files) < p.ChangedFiles {
		b.WriteString("\n " + dim.Render(fmt.Sprintf("GitHub listed %d of %d files; d shows the whole diff",
			len(d.Files), p.ChangedFiles)))
	}
	return b.String()
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	bodyH := max(1, m.height-2)
	var body []string
	switch {
	case m.help:
		body = helpLines(m.width)
	case len(m.prs) == 0:
		body = m.emptyLines()
	default:
		body = m.bodyLines(m.layout())
	}
	return m.header() + "\n" + strings.Join(fitLines(body, m.width, bodyH), "\n") + "\n" + m.footer()
}

func (m model) header() string {
	left := " " + strong.Render("prboom")
	if m.repoName != "" {
		left += " " + dim.Render(m.repoName)
	}
	left += "   "
	for i, s := range scopes {
		label := s.Label
		if m.width < 110 {
			label, _, _ = strings.Cut(label, " ")
		}
		if m.lists[i] != nil || m.have[i] {
			label += fmt.Sprintf(" %d", len(m.lists[i]))
		}
		st := dim
		if i == m.scope {
			st = strong
		}
		left += faint.Render(fmt.Sprint(i+1)) + " " + st.Render(label) + "   "
	}

	var chips []string
	if q := m.query.Value(); q != "" && !m.filtering {
		chips = append(chips, amber.Render("/"+q))
	}
	if m.only != "" {
		chips = append(chips, amber.Render("@"+m.only))
	}
	if m.opt.author != "" {
		chips = append(chips, amber.Render("-A "+m.opt.author))
	}
	if m.opt.sortBy != "recency" {
		chips = append(chips, amber.Render("by "+m.opt.sortBy))
	}
	left += strings.Join(chips, " ")

	var right string
	switch {
	case m.loading[m.scope]:
		right = m.spin.View() + dim.Render(" refreshing")
	case m.errs[m.scope] != nil && len(m.all) > 0:
		right = errSty.Render("refresh failed")
	case !m.at[m.scope].IsZero():
		right = dim.Render("updated " + short(time.Since(m.at[m.scope])))
	}
	if len(m.prs) > 0 {
		right = dim.Render(fmt.Sprintf("%d/%d", m.idx+1, len(m.prs))) + "   " + right
	}
	right += " "

	room := m.width - lipgloss.Width(right)
	return fit(fit(left, max(0, room-1))+" "+right, m.width)
}

func (m model) footer() string {
	if m.filtering {
		return fit(" "+m.query.View(), m.width)
	}
	if m.flash != "" {
		st := green
		if m.flashErr {
			st = errSty
		}
		return fit(" "+st.Render(m.flash), m.width)
	}
	// Most needed first, since a narrow terminal cuts from the right; ? stays
	// on screen to find the rest.
	hints := [][2]string{
		{"⏎", "walk"}, {"t", "task"}, {"/", "filter"}, {"tab", "view"}, {"?", "help"}, {"q", "quit"},
		{"d", "diff"}, {"o", "browser"}, {"1-3", "list"}, {"s", "sort"}, {"f", "author"},
	}
	var b strings.Builder
	b.WriteString(" ")
	for _, h := range hints {
		b.WriteString(accent.Render(h[0]) + " " + dim.Render(h[1]) + "  ")
	}
	return fit(b.String(), m.width)
}

func (m model) emptyLines() []string {
	where := ""
	if m.repoName != "" {
		where = " in " + m.repoName
	}
	label := scopes[m.scope].Label
	switch {
	case m.loading[m.scope] && m.lists[m.scope] == nil:
		return []string{"", "  " + m.spin.View() + dim.Render(" reading "+label+" pull requests"+where+"…")}
	case m.errs[m.scope] != nil && len(m.all) == 0:
		return []string{"", "  " + errSty.Render(m.errs[m.scope].Error()), "", "  " + dim.Render("r retry   q quit")}
	case len(m.all) > 0:
		return []string{"", "  " + dim.Render("No PR matches that."), "", "  " + dim.Render("esc clears it")}
	}
	hint := "1 review requested   2 all open   3 mine   r refresh"
	if m.opt.repo == "" {
		hint += "   -R owner/name for another repo"
	}
	return []string{"", "  " + dim.Render("Nothing in "+label+where+"."), "", "  " + dim.Render(hint)}
}

func (m model) bodyLines(l layout) []string {
	list := m.listLines(l)
	if !l.preview {
		return list
	}
	det := m.detailLines(l)
	if l.side {
		sep := faint.Render(" │ ")
		out := make([]string, l.listH)
		for i := range out {
			out[i] = list[i] + sep + det[i]
		}
		return out
	}
	return append(append(list, faint.Render(strings.Repeat("─", m.width))), det...)
}

func (m model) listLines(l layout) []string {
	numW := 0
	for _, p := range m.prs {
		numW = max(numW, len(fmt.Sprint(p.Number))+1)
	}
	out := make([]string, 0, l.listH)
	for i := scrolled(m.offset, m.idx, len(m.prs), l.listH); i < len(m.prs) && len(out) < l.listH; i++ {
		out = append(out, m.row(m.prs[i], i == m.idx, l.listW, numW))
	}
	return fitLines(out, l.listW, l.listH)
}

func (m model) row(p PR, sel bool, w, numW int) string {
	type cell struct {
		st lipgloss.Style
		s  string
	}

	mark, numSt, titleSt := "  ", dim, lipgloss.NewStyle()
	if sel {
		mark, numSt, titleSt = "▌ ", accent, bold
	}
	if p.IsDraft {
		titleSt = titleSt.Inherit(dim)
	}
	glyph, glyphSt := checkGlyph(p.Checks)
	cells := []cell{{accent, mark}, {glyphSt, glyph + " "}, {numSt, pad("#"+fmt.Sprint(p.Number), numW) + " "}}

	showAuthor, showSize := w >= 64, w >= 84
	titleW := w - (2 + 2 + numW + 1 + 4)
	if showAuthor {
		titleW -= 14
	}
	if showSize {
		titleW -= 13
	}
	if r, ok := m.reviews[p.Number]; ok {
		cells = append(cells, cell{accent, r.Label() + " "})
		titleW -= len(r.Label()) + 1
	}
	if p.IsDraft {
		cells = append(cells, cell{dim, "draft "})
		titleW -= 6
	}
	cells = append(cells, cell{titleSt, fit(p.Title, max(titleW, 0))})
	if showAuthor {
		cells = append(cells, cell{dim, " " + fit(p.Author.Login, 13)})
	}
	if showSize {
		cells = append(cells, cell{dim, " " + fit(fmt.Sprintf("+%d -%d", p.Additions, p.Deletions), 12)})
	}
	cells = append(cells, cell{dim, fmt.Sprintf(" %3s", ago(p.UpdatedAt))})

	var b strings.Builder
	for _, c := range cells {
		st := c.st
		if sel {
			st = st.Background(selBg)
		}
		b.WriteString(st.Render(c.s))
	}
	return fit(b.String(), w)
}

func checkGlyph(state string) (string, lipgloss.Style) {
	switch state {
	case "red":
		return "✗", red
	case "green":
		return "✓", green
	case "pending":
		return "•", amber
	}
	return " ", dim
}

func (m model) detailLines(l layout) []string {
	p, ok := m.current()
	if !ok {
		return fitLines(nil, l.detW, l.detH)
	}
	d, hasDetail := m.details[p.Number]

	meta := fmt.Sprintf("%s · updated %s ago · %s → %s", p.Author.Login, ago(p.UpdatedAt), p.HeadRefName, p.BaseRefName)
	if p.IsCrossRepository {
		meta += " · fork"
	}
	if p.IsDraft {
		meta += " · draft"
	}

	var status []string
	switch p.Checks {
	case "green":
		status = append(status, green.Render("✓ checks pass"))
	case "red":
		status = append(status, red.Render("✗ checks failing"))
	case "pending":
		status = append(status, amber.Render("• checks running"))
	default:
		status = append(status, dim.Render("no checks"))
	}
	switch p.ReviewDecision {
	case "APPROVED":
		status = append(status, green.Render("approved"))
	case "CHANGES_REQUESTED":
		status = append(status, red.Render("changes requested"))
	case "REVIEW_REQUIRED":
		status = append(status, amber.Render("review required"))
	}
	if hasDetail && d.Mergeable == "CONFLICTING" {
		status = append(status, red.Render("conflicts"))
	}
	if r, ok := m.reviews[p.Number]; ok {
		status = append(status, accent.Render(r.Detail()))
	}

	var tabLine strings.Builder
	for i, t := range tabs {
		switch i {
		case 1:
			t += fmt.Sprintf(" %d", p.ChangedFiles)
		case 2:
			t += fmt.Sprintf(" +%d -%d", p.Additions, p.Deletions)
		}
		if i == m.tab {
			tabLine.WriteString(strong.Underline(true).Render(t))
		} else {
			tabLine.WriteString(dim.Render(t))
		}
		tabLine.WriteString("   ")
	}
	right := ""
	switch {
	case m.busy(p.Number):
		right = m.spin.View()
	case m.vp.TotalLineCount() > m.vp.Height:
		right = dim.Render(fmt.Sprintf("%d%%", int(m.vp.ScrollPercent()*100)))
	}

	lines := []string{
		bold.Render(fmt.Sprintf("#%d %s", p.Number, p.Title)),
		dim.Render(meta),
		strings.Join(status, dim.Render(" · ")),
		fit(tabLine.String(), max(0, l.detW-lipgloss.Width(right)-1)) + " " + right,
		faint.Render(strings.Repeat("─", l.detW)),
	}
	lines = append(lines, strings.Split(m.vp.View(), "\n")...)
	return fitLines(lines, l.detW, l.detH)
}

type helpGroup struct {
	title string
	keys  [][2]string
}

// helpLines is two columns where there is room, so it fits a short terminal.
func helpLines(width int) []string {
	groups := []helpGroup{
		{"Move", [][2]string{
			{"j k  ↑ ↓", "next or previous PR"},
			{"g G", "first or last"},
			{"pgup pgdn", "a page of PRs"},
		}},
		{"Narrow", [][2]string{
			{"1 2 3", "review requested, all open, mine"},
			{"/", "filter by title, author, branch or #number"},
			{"f", "only this PR's author; again for everyone"},
			{"s", "sort: recency, author, loc, files"},
			{"r", "refresh"},
		}},
		{"Read", [][2]string{
			{"tab ⇧tab", "overview, files, diff"},
			{"J K", "scroll the detail pane"},
			{"space ^d ^u", "half a page down or up"},
			{"p", "hide or show the detail pane"},
		}},
		{"Act", [][2]string{
			{"⏎", "walk it: worktree, tmux, agent"},
			{"t", "walk it as a TaskYou task"},
			{"d", "the whole diff in delta"},
			{"x", "close a walk: session, worktree, branch"},
			{"L", "learn how you review this repo"},
			{"o", "open it on GitHub"},
			{"y", "copy its URL"},
			{"esc", "clear the filter; with none, quit"},
			{"q", "quit"},
		}},
	}
	column := func(gs []helpGroup) []string {
		var out []string
		for _, g := range gs {
			out = append(out, "  "+strong.Render(g.title))
			for _, k := range g.keys {
				out = append(out, "    "+accent.Render(pad(k[0], 14))+dim.Render(k[1]))
			}
			out = append(out, "")
		}
		return out
	}

	out := []string{""}
	if width < 110 {
		out = append(out, column(groups)...)
	} else {
		left, right := column(groups[:2]), column(groups[2:])
		for i := range max(len(left), len(right)) {
			var l, r string
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			out = append(out, fit(l, 64)+r)
		}
	}
	return append(out, "  "+faint.Render("any key closes this"))
}

// fit makes s exactly w cells wide: cut with an ellipsis, or padded.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…") + "\x1b[0m"
	}
	if n := lipgloss.Width(s); n < w {
		s += strings.Repeat(" ", w-n)
	}
	return s
}

// fitLines makes exactly h lines of exactly w cells, splitting any that hold
// newlines, so no pane can push another out of place.
func fitLines(lines []string, w, h int) []string {
	out := make([]string, 0, h)
	for _, l := range lines {
		for _, s := range strings.Split(l, "\n") {
			if len(out) == h {
				return out
			}
			out = append(out, fit(s, w))
		}
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", max(w, 0)))
	}
	return out
}

// pad counts cells, not bytes, so ⏎ and ⇧ line up with plain letters.
func pad(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
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

// truncLeft keeps the end of a path, which is the part that names the file.
func truncLeft(s string, n int) string {
	r := []rune(s)
	if len(r) <= n || n < 2 {
		return s
	}
	return "…" + string(r[len(r)-n+1:])
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func reviewState(s string) string {
	switch s {
	case "APPROVED":
		return green.Render("approved")
	case "CHANGES_REQUESTED":
		return red.Render("changes requested")
	}
	return dim.Render(strings.ToLower(strings.ReplaceAll(s, "_", " ")))
}

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// prboom view: the code pane. The agent decides what it shows (prboom show),
// and you can take it from there: search, jump to any changed file, leave a
// note for the author on a line, or ask the agent about one. Nothing you do
// here moves the walk; . goes back to what the agent last showed.

const pollEvery = 200 * time.Millisecond

type mode int

const (
	modeCode mode = iota
	modeSearch
	modeComment
	modeAsk
	modeFiles
	modeHelp
)

type (
	pollMsg  struct{}
	flashMsg int
)

type viewer struct {
	root, dir, me string
	mb            string // merge base, found once
	dark          bool

	width, height int
	mode          mode
	input         textinput.Model

	// What the agent asked for, and when that file last changed on disk.
	req     showReq
	stamps  map[string]time.Time
	changed []changed

	fc      fileCode
	view    viewKind
	line    int // line the agent pointed at, 0 for none
	ctx     int
	rows    []row
	cur     int // row the cursor is on
	top     int // first visual line on screen
	cache   map[int][]string
	cacheW  int
	heights []int // screen lines per row at cacheW, cheap to count

	findings []finding
	comments []note

	query   string
	matches []int

	filesQ   string
	filesIdx int

	flash    string
	flashErr bool
	flashN   int
}

func runView(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: prboom view")
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	pid := filepath.Join(dir, "view.pid")
	if err := os.WriteFile(pid, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return err
	}
	defer os.Remove(pid)

	v := newViewer(root, dir, lipgloss.HasDarkBackground())
	v.me = os.Getenv("TMUX_PANE")
	_, err = tea.NewProgram(v, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func newViewer(root, dir string, dark bool) *viewer {
	v := &viewer{
		root: root, dir: dir, mb: mergeBase(root), dark: dark,
		stamps: map[string]time.Time{}, cache: map[int][]string{},
	}
	v.input = textinput.New()
	v.input.Prompt = ""
	return v
}

func (v *viewer) Init() tea.Cmd {
	v.poll()
	if v.req.Path == "" {
		v.changed = changedFiles(v.root, v.mb)
		if len(v.changed) > 0 {
			v.open(v.changed[0].path, 0, 0, viewDiff)
		}
	}
	return tea.Tick(pollEvery, func(time.Time) tea.Msg { return pollMsg{} })
}

// poll picks up whatever changed since last time: a new request from the
// agent, the findings or the notes, or the file on screen being edited.
func (v *viewer) poll() {
	if v.changedOnDisk(filepath.Join(v.dir, "view.json")) {
		var r showReq
		if readJSON(filepath.Join(v.dir, "view.json"), &r) && r.Path != "" {
			v.req = r
			v.back()
		}
	}
	if v.changedOnDisk(filepath.Join(v.dir, "findings.json")) {
		v.findings = nil
		readJSON(filepath.Join(v.dir, "findings.json"), &v.findings)
	}
	if v.changedOnDisk(filepath.Join(v.dir, "comments.json")) {
		v.comments = readComments(v.dir)
		v.cache = map[int][]string{}
	}
	if v.fc.path != "" && v.changedOnDisk(filepath.Join(v.root, v.fc.path)) {
		v.reload()
	}
}

func (v *viewer) changedOnDisk(path string) bool {
	st, err := os.Stat(path)
	var t time.Time
	if err == nil {
		t = st.ModTime()
	}
	old, seen := v.stamps[path]
	v.stamps[path] = t
	return !seen && !t.IsZero() || seen && !t.Equal(old)
}

// back shows what the agent last asked for.
func (v *viewer) back() {
	if v.req.Path == "" {
		return
	}
	view := viewAround
	if v.req.Line == 0 {
		view = viewDiff
	}
	v.open(v.req.Path, v.req.Line, v.req.Ctx, view)
}

func (v *viewer) open(path string, line, ctx int, view viewKind) {
	v.fc = loadFile(v.root, v.mb, path)
	v.stamps[filepath.Join(v.root, path)] = modTime(filepath.Join(v.root, path))
	v.line, v.ctx, v.view = line, ctx, view
	v.recut()
	switch {
	case line > 0:
		v.cur = rowFor(v.rows, line)
	default:
		v.cur = firstChange(v.rows)
	}
	v.top = max(0, v.visualIndex(v.cur)-v.bodyH()/3)
	v.clamp()
}

// reload rereads the file after an edit, keeping the cursor on its line.
func (v *viewer) reload() {
	keep := v.curLine()
	v.fc = loadFile(v.root, v.mb, v.fc.path)
	v.recut()
	if keep > 0 {
		v.cur = rowFor(v.rows, keep)
	}
	v.clamp()
}

func (v *viewer) recut() {
	v.rows, _ = cut(v.fc, v.view, v.line, v.ctx)
	v.cache, v.heights = map[int][]string{}, nil
	v.search()
}

func (v *viewer) curLine() int {
	if v.cur < 0 || v.cur >= len(v.rows) {
		return 0
	}
	r := v.rows[v.cur]
	if r.kind == rowDel {
		return nearestNew(v.rows, v.cur)
	}
	return r.new
}

func modTime(path string) time.Time {
	if st, err := os.Stat(path); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

func (v *viewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width, v.height = msg.Width, msg.Height
		v.cache = map[int][]string{}
		v.clamp()
	case pollMsg:
		v.poll()
		return v, tea.Tick(pollEvery, func(time.Time) tea.Msg { return pollMsg{} })
	case flashMsg:
		if int(msg) == v.flashN {
			v.flash = ""
		}
	case tea.MouseMsg:
		v.mouse(msg)
	case tea.KeyMsg:
		return v, v.key(msg)
	}
	return v, nil
}

func (v *viewer) mouse(msg tea.MouseMsg) {
	if v.mode != modeCode {
		return
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		v.top = max(0, v.top-3)
	case tea.MouseButtonWheelDown:
		v.top += 3
		v.clamp()
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return
		}
		if i := v.rowAt(v.top + msg.Y - v.headH()); i >= 0 {
			v.cur = i
		}
	}
}

func (v *viewer) say(s string, isErr bool) tea.Cmd {
	v.flash, v.flashErr = s, isErr
	v.flashN++
	n := v.flashN
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return flashMsg(n) })
}

func (v *viewer) key(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		return tea.Quit
	}
	switch v.mode {
	case modeHelp:
		v.mode = modeCode
		return nil
	case modeSearch, modeComment, modeAsk:
		return v.typing(msg)
	case modeFiles:
		return v.files(msg)
	}

	// Held j arrives as "jjj"; take each on its own.
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
		var cmds []tea.Cmd
		for _, r := range msg.Runes {
			cmds = append(cmds, v.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
		}
		return tea.Batch(cmds...)
	}

	page := max(1, v.bodyH()/2)
	switch k {
	case "q":
		return tea.Quit
	case "?":
		v.mode = modeHelp
	case "j", "down":
		v.moveCur(1)
	case "k", "up":
		v.moveCur(-1)
	case "ctrl+d", " ", "pgdown":
		v.moveCur(page)
	case "ctrl+u", "pgup":
		v.moveCur(-page)
	case "g", "home":
		v.moveCur(-len(v.rows))
	case "G", "end":
		v.moveCur(len(v.rows))
	case "tab", "shift+tab":
		d := 1
		if k == "shift+tab" {
			d = len(viewNames) - 1
		}
		keep := v.curLine()
		v.view = viewKind((int(v.view) + d) % len(viewNames))
		if v.view == viewAround && v.line == 0 {
			v.view = viewKind((int(v.view) + d) % len(viewNames))
		}
		v.recut()
		if keep > 0 {
			v.cur = rowFor(v.rows, keep)
		}
		v.top = max(0, v.visualIndex(v.cur)-v.bodyH()/3)
		v.clamp()
	case "/":
		v.startInput(modeSearch, v.query, "search")
	case "n", "N":
		return v.nextMatch(k == "n")
	case "esc":
		v.query, v.matches = "", nil
		v.cache = map[int][]string{}
	case "f":
		v.changed = changedFiles(v.root, v.mb)
		v.filesQ, v.filesIdx = "", 0
		v.mode = modeFiles
	case "[", "]":
		return v.jumpFinding(k == "]")
	case ".":
		v.back()
		return v.say("back where the agent is", false)
	case "r":
		v.mb = mergeBase(v.root)
		v.reload()
		return v.say("reloaded", false)
	case "c":
		r, ok := v.row()
		if !ok {
			return v.say("no line here to comment on", true)
		}
		line, side := r.anchor()
		body := ""
		if n, ok := v.noteAt(line, side, "you"); ok {
			body = n.Body
		}
		v.startInput(modeComment, body, fmt.Sprintf("note on %s:%d", v.fc.path, line))
	case "a":
		if v.me == "" {
			return v.say("not in tmux, so there is no agent pane to ask", true)
		}
		if _, ok := v.row(); !ok {
			return v.say("no line here to ask about", true)
		}
		v.startInput(modeAsk, "", "ask the agent")
	}
	return nil
}

func (v *viewer) startInput(m mode, value, placeholder string) {
	v.mode = m
	v.input.SetValue(value)
	v.input.Placeholder = placeholder
	v.input.CursorEnd()
	v.input.Focus()
}

func (v *viewer) typing(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		v.mode = modeCode
		v.input.Blur()
		return nil
	case "enter":
		m, text := v.mode, v.input.Value()
		v.mode = modeCode
		v.input.Blur()
		switch m {
		case modeSearch:
			v.query = text
			v.search()
			return v.nextMatch(true)
		case modeComment:
			return v.saveNote(text)
		case modeAsk:
			return v.ask(text)
		}
		return nil
	}
	var c tea.Cmd
	v.input, c = v.input.Update(msg)
	if v.mode == modeSearch {
		v.query = v.input.Value()
		v.search()
	}
	return c
}

func (v *viewer) saveNote(body string) tea.Cmd {
	r, ok := v.row()
	if !ok {
		return nil
	}
	line, side := r.anchor()
	n := note{Path: v.fc.path, Line: line, Side: side, Body: strings.TrimSpace(body),
		Code: strings.TrimSpace(r.text), By: "you", At: time.Now()}
	v.comments = setComment(v.dir, n)
	v.stamps[filepath.Join(v.dir, "comments.json")] = modTime(filepath.Join(v.dir, "comments.json"))
	v.cache = map[int][]string{}
	if n.Body == "" {
		return v.say("note removed", false)
	}
	return v.say(fmt.Sprintf("noted · %s for the author", plural(len(v.comments), "comment")), false)
}

func (v *viewer) ask(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	r, _ := v.row()
	line, _ := r.anchor()
	msg := fmt.Sprintf("From the code pane, about `%s:%d`:\n\n```\n%s\n```\n\n%s", v.fc.path, line, r.text, text)
	if err := askAgent(v.me, msg); err != nil {
		return v.say("could not reach the agent: "+err.Error(), true)
	}
	return v.say("asked the agent", false)
}

func (v *viewer) files(msg tea.KeyMsg) tea.Cmd {
	list := v.filteredFiles()
	switch msg.String() {
	case "esc":
		v.mode = modeCode
	case "enter":
		v.mode = modeCode
		if v.filesIdx < len(list) {
			v.open(list[v.filesIdx].path, 0, 0, viewDiff)
		}
	case "up", "ctrl+p", "ctrl+k":
		v.filesIdx = max(0, v.filesIdx-1)
	case "down", "ctrl+n", "ctrl+j", "tab":
		v.filesIdx = min(max(0, len(list)-1), v.filesIdx+1)
	case "backspace":
		if r := []rune(v.filesQ); len(r) > 0 {
			v.filesQ = string(r[:len(r)-1])
			v.filesIdx = 0
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			v.filesQ += string(msg.Runes)
			v.filesIdx = 0
		}
	}
	return nil
}

func (v *viewer) filteredFiles() []changed {
	var out []changed
	for _, c := range v.changed {
		if fuzzy(strings.ToLower(v.filesQ), strings.ToLower(c.path)) {
			out = append(out, c)
		}
	}
	return out
}

// fuzzy is true when q's characters appear in s in order.
func fuzzy(q, s string) bool {
	i := 0
	for _, r := range s {
		if i < len(q) && rune(q[i]) == r {
			i++
		}
	}
	return i == len(q)
}

func (v *viewer) jumpFinding(next bool) tea.Cmd {
	if len(v.findings) == 0 {
		return v.say("the agent has not listed any findings", true)
	}
	i := v.findingIndex()
	switch {
	case i < 0 && next:
		i = 0
	case i < 0:
		i = len(v.findings) - 1
	case next:
		i = min(len(v.findings)-1, i+1)
	default:
		i = max(0, i-1)
	}
	f := v.findings[i]
	v.open(f.Path, f.Line, 0, viewAround)
	return v.say(fmt.Sprintf("finding %d of %d · . goes back to the agent's", i+1, len(v.findings)), false)
}

// findingIndex is the finding on screen: same file, same line as where the
// pane is pointed.
func (v *viewer) findingIndex() int {
	for i, f := range v.findings {
		if f.Path == v.fc.path && f.Line == v.line {
			return i
		}
	}
	return -1
}

func (v *viewer) search() {
	v.matches = nil
	if v.query == "" {
		return
	}
	q := v.query
	fold := strings.ToLower(q) == q // smart case
	for i, r := range v.rows {
		t := r.text
		if fold {
			t = strings.ToLower(t)
		}
		if r.kind != rowGap && strings.Contains(t, q) {
			v.matches = append(v.matches, i)
		}
	}
	v.cache = map[int][]string{}
}

func (v *viewer) nextMatch(forward bool) tea.Cmd {
	if len(v.matches) == 0 {
		if v.query != "" {
			return v.say("no match for "+v.query, true)
		}
		return nil
	}
	pick := -1
	if forward {
		for _, m := range v.matches {
			if m > v.cur {
				pick = m
				break
			}
		}
		if pick < 0 {
			pick = v.matches[0]
		}
	} else {
		for i := len(v.matches) - 1; i >= 0; i-- {
			if v.matches[i] < v.cur {
				pick = v.matches[i]
				break
			}
		}
		if pick < 0 {
			pick = v.matches[len(v.matches)-1]
		}
	}
	v.cur = pick
	v.clamp()
	n := 0
	for i, m := range v.matches {
		if m == pick {
			n = i + 1
		}
	}
	return v.say(fmt.Sprintf("/%s  %d of %d", v.query, n, len(v.matches)), false)
}

func (v *viewer) moveCur(d int) {
	v.cur = max(0, min(len(v.rows)-1, v.cur+d))
	v.clamp()
}

func (v *viewer) row() (row, bool) {
	if v.cur < 0 || v.cur >= len(v.rows) || v.rows[v.cur].kind == rowGap {
		return row{}, false
	}
	return v.rows[v.cur], true
}

func (v *viewer) noteAt(line int, side, by string) (note, bool) {
	for _, n := range v.comments {
		if n.Path == v.fc.path && n.Line == line && n.Side == side && (by == "" || n.By == by) {
			return n, true
		}
	}
	return note{}, false
}

// Layout: a header line, the findings strip when there are findings, the
// code, and a footer line.
func (v *viewer) headH() int {
	if len(v.findings) > 0 {
		return 2
	}
	return 1
}

func (v *viewer) bodyH() int { return max(1, v.height-v.headH()-1) }

func (v *viewer) gutterW() int {
	n := 1
	for _, r := range v.fc.rows {
		n = max(n, r.new, r.old)
	}
	return len(strconv.Itoa(n)) + 4 // cursor bar, number, marker, space
}

func (v *viewer) codeW() int { return max(10, v.width-v.gutterW()) }

// lines renders row i to its wrapped screen lines, cached until the width,
// view, search or notes change.
func (v *viewer) lines(i int) []string {
	v.fresh()
	if l, ok := v.cache[i]; ok {
		return l
	}
	l := v.renderRow(i)
	v.cache[i] = l
	return l
}

// fresh drops what was rendered for another width or another set of rows.
func (v *viewer) fresh() {
	if v.cacheW != v.width || v.heights != nil && len(v.heights) != len(v.rows) {
		v.cache, v.cacheW, v.heights = map[int][]string{}, v.width, nil
	}
}

// rowHeight is how many screen lines row i wraps to. Counted from the plain text,
// so scrolling a long file never highlights rows that are off screen.
func (v *viewer) rowHeight(i int) int {
	v.fresh()
	if v.heights == nil {
		v.heights = make([]int, len(v.rows))
	}
	if v.heights[i] == 0 {
		v.heights[i] = 1
		if r := v.rows[i]; r.kind != rowGap {
			v.heights[i] = len(wrapSegs([]seg{{text: expandTabs(r.text)}}, v.codeW()))
		}
	}
	return v.heights[i]
}

func (v *viewer) visualIndex(row int) int {
	n := 0
	for i := 0; i < row && i < len(v.rows); i++ {
		n += v.rowHeight(i)
	}
	return n
}

func (v *viewer) rowAt(visual int) int {
	n := 0
	for i := range v.rows {
		n += v.rowHeight(i)
		if visual < n {
			return i
		}
	}
	return -1
}

// clamp keeps the cursor on screen and the screen inside the file.
func (v *viewer) clamp() {
	if len(v.rows) == 0 {
		v.cur, v.top = 0, 0
		return
	}
	v.cur = max(0, min(len(v.rows)-1, v.cur))
	if v.width == 0 {
		return
	}
	h := v.bodyH()
	start := v.visualIndex(v.cur)
	end := start + v.rowHeight(v.cur)
	if start < v.top {
		v.top = start
	}
	if end > v.top+h {
		v.top = end - h
	}
	total := v.visualIndex(len(v.rows))
	v.top = max(0, min(v.top, total-h))
}

func (v *viewer) View() string {
	if v.width == 0 {
		return ""
	}
	var out []string
	out = append(out, v.header())
	if len(v.findings) > 0 {
		out = append(out, v.strip())
	}
	var body []string
	switch v.mode {
	case modeHelp:
		body = viewHelp()
	case modeFiles:
		body = v.fileList()
	default:
		body = v.code()
	}
	out = append(out, fitLines(body, v.width, v.bodyH())...)
	out = append(out, v.footer())
	return strings.Join(out, "\n")
}

func (v *viewer) header() string {
	if v.fc.path == "" {
		return fit(dim.Render(" waiting for the agent to show something"), v.width)
	}
	what := map[string]string{"A": "new", "D": "deleted", "M": "changed", "": "untouched", "B": "binary"}[v.fc.status]
	var tabs []string
	for i, name := range viewNames {
		if viewKind(i) == viewAround && v.line == 0 {
			continue
		}
		st := dim
		if viewKind(i) == v.view {
			st = strong
		}
		tabs = append(tabs, st.Render(name))
	}
	right := strings.Join(tabs, faint.Render(" · ")) + " "
	room := v.width - lipgloss.Width(right) - 1
	// The end of a path is the part that says which file it is.
	path := truncLeft(v.fc.path, max(1, room-len(what)-2))
	left := " " + bold.Render(path) + " " + dim.Render(what)
	return fit(fit(left, max(0, room))+" "+right, v.width)
}

// strip is the findings, one line: where the agent is, and the rest as dots.
func (v *viewer) strip() string {
	i := v.findingIndex()
	var dots strings.Builder
	for j := range v.findings {
		switch {
		case j == i:
			dots.WriteString(strong.Render("●"))
		default:
			dots.WriteString(faint.Render("●"))
		}
	}
	label := dim.Render("not on a finding · [ ] to step through")
	if i >= 0 {
		label = strong.Render(fmt.Sprintf("%d of %d", i+1, len(v.findings))) + " " + v.findings[i].Title
	}
	return fit(" "+dots.String()+"  "+label, v.width)
}

func (v *viewer) footer() string {
	switch v.mode {
	case modeSearch:
		return fit(accent.Render(" /")+v.input.View(), v.width)
	case modeComment:
		return fit(amber.Render(" 💬 ")+v.input.View()+dim.Render("  ⏎ save · empty removes · esc"), v.width)
	case modeAsk:
		return fit(accent.Render(" ask › ")+v.input.View()+dim.Render("  ⏎ send · esc"), v.width)
	case modeFiles:
		return fit(accent.Render(" files › ")+v.filesQ+dim.Render("▏ ⏎ open · esc"), v.width)
	}
	if v.flash != "" {
		st := dim
		if v.flashErr {
			st = errSty
		}
		return fit(" "+st.Render(v.flash), v.width)
	}
	pos := ""
	if len(v.rows) > 0 {
		pos = fmt.Sprintf("%d%%", 100*(v.cur+1)/len(v.rows))
	}
	mine := 0
	for _, n := range v.comments {
		if n.By == "you" {
			mine++
		}
	}
	keys := dim.Render(" / search · f files · c note · a ask · tab view · . back · ? keys")
	right := dim.Render(fmt.Sprintf("💬 %d  %s ", mine, pos))
	room := v.width - lipgloss.Width(right)
	return fit(fit(keys, max(0, room))+right, v.width)
}

func (v *viewer) code() []string {
	if v.fc.path == "" {
		return []string{"", dim.Render("  The agent puts code here with `pr-show FILE:LINE`."),
			dim.Render("  f lists the files this PR changes.")}
	}
	if v.fc.err != nil {
		return []string{"", errSty.Render("  " + v.fc.err.Error())}
	}
	if v.fc.status == "B" {
		return []string{"", dim.Render("  binary file")}
	}
	if len(v.rows) == 0 {
		return []string{"", dim.Render("  nothing to show in this view · tab for another")}
	}
	var out []string
	h := v.bodyH()
	seen := 0
	for i := range v.rows {
		if hi := v.rowHeight(i); seen+hi <= v.top {
			seen += hi
			continue
		}
		ls := v.lines(i)
		for j, l := range ls {
			if seen+j >= v.top {
				out = append(out, l)
			}
		}
		seen += len(ls)
		if len(out) >= h {
			break
		}
	}
	return out
}

func (v *viewer) fileList() []string {
	list := v.filteredFiles()
	out := []string{dim.Render(fmt.Sprintf(" %d of %s", len(list), plural(len(v.changed), "changed file")))}
	h := v.bodyH() - 1
	start := max(0, v.filesIdx-h+1)
	for i := start; i < len(list) && i < start+h; i++ {
		c := list[i]
		stat := green.Render(fmt.Sprintf("+%d", c.add)) + " " + red.Render(fmt.Sprintf("-%d", c.del))
		mark := "  "
		name := c.path
		if i == v.filesIdx {
			mark = strong.Render("▌ ")
			name = bold.Render(name)
		}
		out = append(out, fit(mark+truncLeft(name, v.width-18)+"  "+stat, v.width))
	}
	return out
}

func viewHelp() []string {
	rows := [][2]string{
		{"j k  ↑ ↓", "move the cursor"},
		{"^d ^u  space", "half a page"},
		{"g G", "top, bottom"},
		{"wheel, click", "scroll, put the cursor on a line"},
		{"tab", "around the line · just the diff · whole file"},
		{"/  n N  esc", "search, next, previous, clear"},
		{"f", "open any file this PR changes"},
		{"[ ]", "previous or next finding"},
		{".", "back to what the agent showed"},
		{"c", "note for the author on this line (again to edit)"},
		{"a", "ask the agent about this line"},
		{"r", "reload"},
		{"q", "quit, leaving a shell"},
	}
	out := []string{"", strong.Render("  code pane")}
	for _, r := range rows {
		out = append(out, "  "+accent.Render(pad(r[0], 14))+" "+r[1])
	}
	return append(out, "", dim.Render("  Nothing here moves the walk. Answer the agent in its own pane."))
}

// Tints behind changed lines, matching pr-show's: the syntax colour stays.
func (v *viewer) tint(k rowKind) lipgloss.TerminalColor {
	switch {
	case k == rowAdd && v.dark:
		return lipgloss.Color("#0f2a18")
	case k == rowDel && v.dark:
		return lipgloss.Color("#2d1416")
	case k == rowAdd:
		return lipgloss.Color("#e6ffec")
	case k == rowDel:
		return lipgloss.Color("#ffebe9")
	}
	return nil
}

func (v *viewer) renderRow(i int) []string {
	r := v.rows[i]
	gw, cw := v.gutterW(), v.codeW()
	numW := gw - 4
	if r.kind == rowGap {
		n, _ := strconv.Atoi(r.text)
		return []string{faint.Render(strings.Repeat(" ", gw) + fmt.Sprintf("⋯ %s", plural(n, "line")))}
	}

	bg := v.tint(r.kind)
	// An added file is all additions; tinting every line says nothing.
	if v.fc.status == "A" && r.kind == rowAdd {
		bg = nil
	}
	base := lipgloss.NewStyle()
	if bg != nil {
		base = base.Background(bg)
	}

	num := r.new
	marker := " "
	numSt := faint
	switch r.kind {
	case rowAdd:
		marker = green.Render("+")
	case rowDel:
		num, marker = r.old, red.Render("-")
	}
	bar := " "
	if i == v.cur && v.mode != modeFiles {
		bar = strong.Render("▌")
		numSt = accent
	}
	line, side := r.anchor()
	_, noted := v.noteAt(line, side, "")
	numText := fmt.Sprintf("%*d", numW, num)
	if noted {
		numText = strings.Repeat(" ", max(0, numW-2)) + "💬" // two cells wide
		numSt = amber
	}
	gutter := bar + numSt.Render(numText) + " " + marker + " "
	for j := range v.matches {
		if v.matches[j] == i {
			gutter = bar + numSt.Render(numText) + " " + marker + amber.Render("•")
		}
	}

	segs := highlight(v.fc.path, expandTabs(r.text), v.dark)
	wrapped := wrapSegs(segs, cw)
	out := make([]string, len(wrapped))
	for j, w := range wrapped {
		var b strings.Builder
		width := 0
		for _, s := range w {
			st := s.st.Inherit(base)
			if v.query != "" {
				// matched text stands out, the rest keeps its colour
				b.WriteString(markMatches(s.text, v.query, st))
			} else {
				b.WriteString(st.Render(s.text))
			}
			width += ansi.StringWidth(s.text)
		}
		if width < cw {
			b.WriteString(base.Render(strings.Repeat(" ", cw-width)))
		}
		g := gutter
		if j > 0 {
			g = strings.Repeat(" ", gw)
		}
		out[j] = g + b.String()
	}
	return out
}

func markMatches(text, q string, st lipgloss.Style) string {
	fold := strings.ToLower(q) == q
	hay := text
	if fold {
		hay = strings.ToLower(text)
	}
	hit := st.Reverse(true)
	var b strings.Builder
	for {
		i := strings.Index(hay, q)
		if i < 0 {
			b.WriteString(st.Render(text))
			return b.String()
		}
		b.WriteString(st.Render(text[:i]))
		b.WriteString(hit.Render(text[i : i+len(q)]))
		text, hay = text[i+len(q):], hay[i+len(q):]
	}
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

type seg struct {
	text string
	st   lipgloss.Style
}

// highlight colours one line of code. Line by line loses a string or comment
// that spans lines, which in a review pane is a fair price for never having to
// re-lex a whole file.
func highlight(path, text string, dark bool) []seg {
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		return []seg{{text: text}}
	}
	lexer = chroma.Coalesce(lexer)
	name := "github"
	if dark {
		name = "github-dark"
	}
	style := styles.Get(name)
	it, err := lexer.Tokenise(nil, text)
	if err != nil {
		return []seg{{text: text}}
	}
	var segs []seg
	for _, tok := range it.Tokens() {
		t := strings.TrimRight(tok.Value, "\n")
		if t == "" {
			continue
		}
		e := style.Get(tok.Type)
		st := lipgloss.NewStyle()
		if e.Colour.IsSet() {
			st = st.Foreground(lipgloss.Color(e.Colour.String()))
		}
		if e.Bold == chroma.Yes {
			st = st.Bold(true)
		}
		if e.Italic == chroma.Yes {
			st = st.Italic(true)
		}
		segs = append(segs, seg{t, st})
	}
	return segs
}

// wrapSegs breaks coloured text into lines of at most w cells.
func wrapSegs(segs []seg, w int) [][]seg {
	lines := [][]seg{nil}
	used := 0
	for _, s := range segs {
		var cur strings.Builder
		for _, r := range s.text {
			rw := ansi.StringWidth(string(r))
			if used+rw > w && used > 0 {
				if cur.Len() > 0 {
					lines[len(lines)-1] = append(lines[len(lines)-1], seg{cur.String(), s.st})
					cur.Reset()
				}
				lines = append(lines, nil)
				used = 0
			}
			cur.WriteRune(r)
			used += rw
		}
		if cur.Len() > 0 {
			lines[len(lines)-1] = append(lines[len(lines)-1], seg{cur.String(), s.st})
		}
	}
	return lines
}

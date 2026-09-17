package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const sampleDiff = `diff --git a/a.go b/a.go
index 111..222 100644
--- a/a.go
+++ b/a.go
@@ -1,6 +1,6 @@
 package a

-func old() {}
+func new() {}

 func keep() {}
 // end
`

func TestParseDiff(t *testing.T) {
	status, rows := parseDiff(sampleDiff)
	if status != "M" {
		t.Fatalf("status %q", status)
	}
	want := []row{
		{rowCtx, 1, 1, "package a"},
		{rowCtx, 2, 2, ""},
		{rowDel, 3, 0, "func old() {}"},
		{rowAdd, 0, 3, "func new() {}"},
		{rowCtx, 4, 4, ""},
		{rowCtx, 5, 5, "func keep() {}"},
		{rowCtx, 6, 6, "// end"},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
}

func TestParseDiffStatus(t *testing.T) {
	if s, _ := parseDiff("diff --git a/x b/x\nnew file mode 100644\n@@ -0,0 +1 @@\n+hi\n"); s != "A" {
		t.Errorf("added: %q", s)
	}
	if s, _ := parseDiff("diff --git a/x b/x\ndeleted file mode 100644\n@@ -1 +0,0 @@\n-hi\n"); s != "D" {
		t.Errorf("deleted: %q", s)
	}
	if s, rows := parseDiff("diff --git a/x b/x\nBinary files a/x and b/x differ\n"); s != "B" || rows != nil {
		t.Errorf("binary: %q %v", s, rows)
	}
}

func longFile() fileCode {
	var rows []row
	for i := 1; i <= 100; i++ {
		rows = append(rows, row{kind: rowCtx, old: i, new: i, text: "line"})
	}
	// line 50 replaced
	rows[49] = row{kind: rowAdd, new: 50, text: "new 50"}
	rows = append(rows[:49], append([]row{{kind: rowDel, old: 50, text: "old 50"}}, rows[49:]...)...)
	return fileCode{path: "x.go", status: "M", rows: rows}
}

func TestCutViews(t *testing.T) {
	fc := longFile()

	around, _ := cut(fc, viewAround, 50, 5)
	if first, last := around[0].new, around[len(around)-1].new; first != 45 || last != 55 {
		t.Errorf("around 50±5 spans %d..%d", first, last)
	}
	hasDel := false
	for _, r := range around {
		hasDel = hasDel || r.kind == rowDel
	}
	if !hasDel {
		t.Error("around should keep the deleted line beside 50")
	}

	diff, idx := cut(fc, viewDiff, 0, 0)
	if diff[0].kind != rowGap || diff[len(diff)-1].kind != rowGap {
		t.Errorf("diff view should open and close with gaps: %+v ... %+v", diff[0], diff[len(diff)-1])
	}
	if len(diff) != 2+3+2+3 { // gap, 3 before, del+add, 3 after, gap
		t.Errorf("diff view has %d rows", len(diff))
	}
	if idx[0] != -1 || idx[1] != 46 {
		t.Errorf("idx %v", idx[:3])
	}

	file, _ := cut(fc, viewFile, 0, 0)
	if len(file) != len(fc.rows) {
		t.Errorf("file view %d rows, want %d", len(file), len(fc.rows))
	}

	// No line to be around: the diff instead.
	if noLine, _ := cut(fc, viewAround, 0, 0); len(noLine) != len(diff) {
		t.Error("around with no line should fall back to the diff")
	}
}

func TestRowFor(t *testing.T) {
	fc := longFile()
	rows, _ := cut(fc, viewFile, 0, 0)
	if i := rowFor(rows, 50); rows[i].kind != rowAdd || rows[i].new != 50 {
		t.Errorf("rowFor 50 landed on %+v", rows[i])
	}
	if i := rowFor(rows, 1000); i != len(rows)-1 {
		t.Errorf("past the end should clamp, got %d", i)
	}
	deleted := fileCode{status: "D", rows: []row{{kind: rowDel, old: 1, text: "a"}, {kind: rowDel, old: 2, text: "b"}}}
	if i := rowFor(deleted.rows, 2); i != 1 {
		t.Errorf("deleted file rowFor 2 = %d", i)
	}
	if around, _ := cut(deleted, viewAround, 2, 1); len(around) != 2 {
		t.Errorf("deleted file around: %d rows", len(around))
	}
}

func TestParseLoc(t *testing.T) {
	for in, want := range map[string]struct {
		p string
		n int
	}{
		"app/x.rb:120": {"app/x.rb", 120},
		"app/x.rb":     {"app/x.rb", 0},
		"a:b.rb":       {"a:b.rb", 0},
		"x.rb:0":       {"x.rb:0", 0},
	} {
		if p, n := parseLoc(in); p != want.p || n != want.n {
			t.Errorf("parseLoc(%q) = %q %d", in, p, n)
		}
	}
}

func TestParseFindings(t *testing.T) {
	in := "app/x.rb:120 Delete recalculate_legacy (-21)\n\n  lib/y.rb   Duplicates Foo\n"
	fs, err := parseFindings(strings.NewReader(in), func(p string) string { return "root/" + p })
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 2 {
		t.Fatalf("%+v", fs)
	}
	if fs[0] != (finding{"root/app/x.rb", 120, "Delete recalculate_legacy (-21)"}) {
		t.Errorf("%+v", fs[0])
	}
	if fs[1] != (finding{"root/lib/y.rb", 0, "Duplicates Foo"}) {
		t.Errorf("%+v", fs[1])
	}
}

func TestSetComment(t *testing.T) {
	dir := t.TempDir()
	setComment(dir, note{Path: "b.go", Line: 3, Side: "RIGHT", Body: "b3", By: "you"})
	setComment(dir, note{Path: "a.go", Line: 9, Side: "RIGHT", Body: "a9", By: "you"})
	setComment(dir, note{Path: "a.go", Line: 9, Side: "RIGHT", Body: "agent's", By: "agent"})
	ns := setComment(dir, note{Path: "a.go", Line: 9, Side: "RIGHT", Body: "a9 edited", By: "you"})
	if len(ns) != 3 || ns[0].Path != "a.go" || ns[len(ns)-1].Path != "b.go" {
		t.Fatalf("sorted by place, one per author: %+v", ns)
	}
	var mine note
	for _, n := range ns {
		if n.By == "you" && n.Path == "a.go" {
			mine = n
		}
	}
	if mine.Body != "a9 edited" {
		t.Errorf("edit should replace, got %+v", ns)
	}
	ns = setComment(dir, note{Path: "b.go", Line: 3, Side: "RIGHT", Body: "  ", By: "you"})
	if len(ns) != 2 || len(readComments(dir)) != 2 {
		t.Errorf("an empty body removes: %+v", ns)
	}
}

func TestWrapSegs(t *testing.T) {
	lines := wrapSegs([]seg{{text: "abcd"}, {text: "efgh"}, {text: "ij"}}, 3)
	var got []string
	for _, l := range lines {
		var b strings.Builder
		for _, s := range l {
			b.WriteString(s.text)
		}
		got = append(got, b.String())
	}
	if strings.Join(got, "|") != "abc|def|ghi|j" {
		t.Errorf("wrapped to %q", got)
	}
	if n := len(wrapSegs(nil, 10)); n != 1 {
		t.Errorf("an empty line is still one line, got %d", n)
	}
}

func TestFuzzy(t *testing.T) {
	if !fuzzy("amo", "app/models/offer.rb") || fuzzy("zz", "app/models/offer.rb") || !fuzzy("", "x") {
		t.Error("fuzzy")
	}
}

// A real repo: a PR branch off main that changes one file, adds one and
// deletes one, then the pane driven the way the agent and you would.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	var src strings.Builder
	for i := 1; i <= 80; i++ {
		src.WriteString("// line\n")
	}
	write("app/offer.go", "package app\n\nfunc recalculateLegacy() {}\n"+src.String())
	write("gone.go", "package app\n")
	run("add", ".")
	run("commit", "-q", "-m", "base")
	run("checkout", "-q", "-b", "pr")
	write("app/offer.go", "package app\n\nfunc recalculate() {}\n"+src.String())
	write("app/new.go", "package app\n\nfunc Added() {}\n")
	run("rm", "-q", "gone.go")
	run("add", ".")
	run("commit", "-q", "-m", "pr")
	t.Setenv("PR_BASE", "main")
	return dir
}

func TestLoadFileAndChanged(t *testing.T) {
	root := gitRepo(t)
	mb := mergeBase(root)

	cs := changedFiles(root, mb)
	status := map[string]string{}
	for _, c := range cs {
		status[c.path] = c.status
	}
	if status["app/offer.go"] != "M" || status["app/new.go"] != "A" || status["gone.go"] != "D" {
		t.Errorf("changed: %+v", cs)
	}

	fc := loadFile(root, mb, "app/offer.go")
	if fc.err != nil || fc.status != "M" || len(fc.rows) != 84 { // 83 lines + the deleted one
		t.Fatalf("offer.go: %v %q %d rows", fc.err, fc.status, len(fc.rows))
	}
	if fc = loadFile(root, mb, "app/new.go"); fc.status != "A" {
		t.Errorf("new.go status %q", fc.status)
	}
	if fc = loadFile(root, mb, "gone.go"); fc.status != "D" || len(fc.rows) != 1 {
		t.Errorf("gone.go %q %d", fc.status, len(fc.rows))
	}

	// An edit made during the walk shows up without a commit.
	if err := os.WriteFile(filepath.Join(root, "app/new.go"), []byte("package app\n\nfunc Added() { fixed() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fc = loadFile(root, mb, "app/new.go")
	if !strings.Contains(fc.rows[2].text, "fixed") {
		t.Errorf("uncommitted fix missing: %+v", fc.rows)
	}

	if rel, err := resolvePath(root, root, "offer.go"); err != nil || rel != "app/offer.go" {
		t.Errorf("resolve by tail: %q %v", rel, err)
	}
	if rel, err := resolvePath(root, root, "gone.go"); err != nil || rel != "gone.go" {
		t.Errorf("resolve a deleted file: %q %v", rel, err)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func paneModel(t *testing.T, root string) *viewer {
	t.Helper()
	dir, err := walkDir(root)
	if err != nil {
		t.Fatal(err)
	}
	v := newViewer(root, dir, true)
	v.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	return v
}

func press(v *viewer, ks ...string) {
	special := map[string]tea.KeyType{"tab": tea.KeyTab, "esc": tea.KeyEsc, "enter": tea.KeyEnter}
	for _, k := range ks {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		if kt, ok := special[k]; ok {
			msg = tea.KeyMsg{Type: kt}
		}
		v.Update(msg)
	}
}

func screen(v *viewer) string { return ansi.Strip(v.View()) }

func TestPaneFollowsTheAgent(t *testing.T) {
	root := gitRepo(t)
	chdir(t, root)
	t.Setenv("TMUX", "")

	// The agent lists findings and shows one. Outside tmux show still records
	// the request, then says there is no pane to put it in.
	if err := runFindingsFrom(strings.NewReader("offer.go:3 Delete recalculateLegacy\napp/new.go:3 Unused\n")); err != nil {
		t.Fatal(err)
	}
	if err := runShow([]string{"offer.go:3", "5"}); err == nil || !strings.Contains(err.Error(), "not inside tmux") {
		t.Fatalf("show outside tmux: %v", err)
	}

	v := paneModel(t, root)
	v.Init()
	out := screen(v)
	for _, want := range []string{"app/offer.go", "changed", "1 of 2", "Delete recalculateLegacy", "func recalculate() {}", "func recalculateLegacy() {}"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen lacks %q:\n%s", want, out)
		}
	}
	if r, _ := v.row(); r.new != 3 || r.kind != rowAdd {
		t.Errorf("cursor on %+v, want the added line 3", r)
	}

	// Next finding, then back to where the agent is.
	press(v, "]")
	if v.fc.path != "app/new.go" || !strings.Contains(screen(v), "2 of 2") {
		t.Errorf("] went to %s", v.fc.path)
	}
	press(v, ".")
	if v.fc.path != "app/offer.go" {
		t.Errorf(". went to %s", v.fc.path)
	}

	// Search.
	press(v, "/", "l", "i", "n", "e", "enter")
	if r, _ := v.row(); r.text != "// line" {
		t.Errorf("search landed on %+v", r)
	}

	// A note for the author on the cursor line, visible to the agent.
	for v.rows[v.cur].kind != rowAdd {
		press(v, "k")
	}
	press(v, "c", "enter") // empty: nothing saved
	press(v, "c")
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("why rename this?")})
	press(v, "enter")
	ns := readComments(v.dir)
	if len(ns) != 1 || ns[0] != (note{Path: "app/offer.go", Line: 3, Side: "RIGHT", Body: "why rename this?",
		Code: "func recalculate() {}", By: "you", At: ns[0].At}) {
		t.Fatalf("comments: %+v", ns)
	}
	if !strings.Contains(screen(v), "💬") {
		t.Error("a noted line should show 💬")
	}
	for i, l := range strings.Split(v.View(), "\n") {
		if n := ansi.StringWidth(l); n > v.width {
			t.Errorf("line %d with a note is %d wide in %d", i, n, v.width)
		}
	}

	// The deleted line takes a LEFT note.
	for v.rows[v.cur].kind != rowDel {
		press(v, "k")
	}
	press(v, "c")
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("was this used?")})
	press(v, "enter")
	if ns = readComments(v.dir); len(ns) != 2 || ns[0].Side != "LEFT" || ns[0].Line != 3 {
		t.Errorf("left note: %+v", ns)
	}

	// Views keep the cursor's line.
	press(v, "tab")
	if v.view != viewDiff {
		t.Errorf("tab went to %v", v.view)
	}
	press(v, "tab")
	if v.view != viewFile || len(v.rows) != len(v.fc.rows) {
		t.Errorf("second tab: %v with %d rows", v.view, len(v.rows))
	}

	// Any changed file.
	press(v, "f", "g", "o", "n", "e", "enter")
	if v.fc.path != "gone.go" || !strings.Contains(screen(v), "deleted") {
		t.Errorf("files picker opened %q", v.fc.path)
	}

	// The agent shows something new: the pane follows on its next poll.
	time.Sleep(10 * time.Millisecond)
	req := showReq{Path: "app/new.go", Line: 3, At: time.Now()}
	writeJSON(filepath.Join(v.dir, "view.json"), req)
	future := time.Now().Add(time.Second)
	_ = os.Chtimes(filepath.Join(v.dir, "view.json"), future, future)
	v.Update(pollMsg{})
	if v.fc.path != "app/new.go" {
		t.Errorf("did not follow the agent: %s", v.fc.path)
	}

	// An edit in the worktree reloads the file on screen.
	if err := os.WriteFile(filepath.Join(root, "app/new.go"), []byte("package app\n\nfunc Added() { edited() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(filepath.Join(root, "app/new.go"), future, future)
	v.Update(pollMsg{})
	if !strings.Contains(screen(v), "edited()") {
		t.Errorf("edit not picked up:\n%s", screen(v))
	}

	// The agent's own comment joins yours.
	if err := runComment([]string{"offer.go:5", "agent", "says"}); err != nil {
		t.Fatal(err)
	}
	if ns = readComments(v.dir); len(ns) != 3 || ns[2].By != "agent" || ns[2].Code != "// line" {
		t.Errorf("agent comment: %+v", ns)
	}
}

func TestPaneFitsItsSize(t *testing.T) {
	root := gitRepo(t)
	chdir(t, root)
	v := paneModel(t, root)
	v.Init() // no request: opens the first changed file
	if v.fc.path == "" {
		t.Fatal("nothing opened")
	}
	for _, w := range []int{30, 60, 120} {
		v.Update(tea.WindowSizeMsg{Width: w, Height: 12})
		for _, mode := range []string{"", "?", "f"} {
			if mode != "" {
				press(v, mode)
			}
			if mode == "" && !strings.Contains(ansi.Strip(v.View()), "new.go") && !strings.Contains(ansi.Strip(v.View()), "offer.go") {
				t.Errorf("width %d: header lost the file name:\n%s", w, ansi.Strip(v.View()))
			}
			lines := strings.Split(v.View(), "\n")
			if len(lines) != 12 {
				t.Errorf("width %d mode %q: %d lines", w, mode, len(lines))
			}
			for i, l := range lines {
				if n := ansi.StringWidth(l); n > w {
					t.Errorf("width %d mode %q line %d is %d wide", w, mode, i, n)
				}
			}
			press(v, "esc")
		}
	}
}

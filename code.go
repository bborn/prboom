package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type rowKind int

const (
	rowCtx rowKind = iota
	rowAdd
	rowDel
	rowGap // lines the diff view leaves out
)

// A row is one line of a file as the PR sees it. old and new are its line
// numbers on each side, 0 where it has none.
type row struct {
	kind     rowKind
	old, new int
	text     string
}

// fileCode is one file loaded for the pane: every line, in place, with the
// PR's changes marked. Every view is cut from these rows, so a cursor keeps its
// line when the view changes.
type fileCode struct {
	path   string
	status string // A added, D deleted, M changed, "" untouched, B binary
	rows   []row
	err    error
}

// reviewBase is what the PR is measured against: PR_BASE, else what pr-open
// recorded, else origin/main, else HEAD.
func reviewBase(root string) string {
	if b := os.Getenv("PR_BASE"); b != "" {
		return b
	}
	if out, err := git(root, "rev-parse", "--path-format=absolute", "--git-dir"); err == nil {
		if b, err := os.ReadFile(filepath.Join(strings.TrimSpace(out), "pr-review-base")); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				return s
			}
		}
	}
	if _, err := git(root, "rev-parse", "--verify", "-q", "origin/main"); err == nil {
		return "origin/main"
	}
	return "HEAD"
}

// mergeBase is where the PR branched off. Diffing from it to the working tree
// shows what GitHub shows plus any fix made during the walk.
func mergeBase(root string) string {
	base := reviewBase(root)
	if out, err := git(root, "merge-base", base, "HEAD"); err == nil {
		return strings.TrimSpace(out)
	}
	return base
}

type changed struct {
	path     string
	status   string
	add, del int
}

func changedFiles(root, mb string) []changed {
	out, err := git(root, "diff", "--no-ext-diff", "--numstat", "--no-renames", mb)
	if err != nil {
		return nil
	}
	status := map[string]string{}
	if ns, err := git(root, "diff", "--no-ext-diff", "--name-status", "--no-renames", mb); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(ns), "\n") {
			if s, p, ok := strings.Cut(l, "\t"); ok {
				status[p] = s[:1]
			}
		}
	}
	var cs []changed
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.SplitN(l, "\t", 3)
		if len(f) != 3 {
			continue
		}
		a, _ := strconv.Atoi(f[0])
		d, _ := strconv.Atoi(f[1])
		cs = append(cs, changed{path: f[2], status: status[f[2]], add: a, del: d})
	}
	return cs
}

// loadFile reads a file with the whole diff against mb in place.
func loadFile(root, mb, rel string) fileCode {
	fc := fileCode{path: rel}
	diff, err := git(root, "diff", "--no-color", "--no-ext-diff", "--no-renames", "-U1000000", mb, "--", rel)
	if err != nil {
		fc.err = err
		return fc
	}
	if strings.TrimSpace(diff) == "" {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			fc.err = err
			return fc
		}
		fc.rows = sourceRows(string(b))
		return fc
	}
	fc.status, fc.rows = parseDiff(diff)
	return fc
}

func sourceRows(src string) []row {
	src = strings.TrimSuffix(src, "\n")
	lines := strings.Split(src, "\n")
	rows := make([]row, len(lines))
	for i, l := range lines {
		rows[i] = row{kind: rowCtx, old: i + 1, new: i + 1, text: l}
	}
	return rows
}

// parseDiff reads a unified diff of one file.
func parseDiff(diff string) (string, []row) {
	status := "M"
	var rows []row
	old, nw := 0, 0
	inHunk := false
	for _, l := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		if !inHunk {
			switch {
			case strings.HasPrefix(l, "new file mode"):
				status = "A"
			case strings.HasPrefix(l, "deleted file mode"):
				status = "D"
			case strings.HasPrefix(l, "Binary files"):
				return "B", nil
			}
		}
		if strings.HasPrefix(l, "@@") {
			inHunk = true
			old, nw = hunkStart(l)
			continue
		}
		if !inHunk {
			continue
		}
		switch {
		case strings.HasPrefix(l, "+"):
			rows = append(rows, row{kind: rowAdd, new: nw, text: l[1:]})
			nw++
		case strings.HasPrefix(l, "-"):
			rows = append(rows, row{kind: rowDel, old: old, text: l[1:]})
			old++
		case strings.HasPrefix(l, `\`):
		default:
			rows = append(rows, row{kind: rowCtx, old: old, new: nw, text: strings.TrimPrefix(l, " ")})
			old++
			nw++
		}
	}
	return status, rows
}

// hunkStart reads @@ -a,b +c,d @@.
func hunkStart(h string) (int, int) {
	f := strings.Fields(h)
	num := func(s string) int {
		s, _, _ = strings.Cut(strings.TrimLeft(s, "-+"), ",")
		n, _ := strconv.Atoi(s)
		return n
	}
	if len(f) < 3 {
		return 1, 1
	}
	return num(f[1]), num(f[2])
}

type viewKind int

const (
	viewAround viewKind = iota // the lines near where the agent pointed
	viewDiff                   // only the changes, with a little context
	viewFile                   // the whole file
)

var viewNames = []string{"around", "diff", "file"}

// cut makes a view's rows. idx maps each back to its index in fc.rows, -1 for
// a gap.
func cut(fc fileCode, v viewKind, line, ctx int) (rows []row, idx []int) {
	all := fc.rows
	switch v {
	case viewAround:
		if line <= 0 {
			return cut(fc, viewDiff, 0, 0)
		}
		if ctx <= 0 {
			ctx = 30
		}
		lo, hi := line-ctx, line+ctx
		for i, r := range all {
			n := r.new
			switch {
			case fc.status == "D":
				n = r.old
			case r.kind == rowDel:
				n = nearestNew(all, i)
			}
			if n >= lo && n <= hi {
				rows, idx = append(rows, r), append(idx, i)
			}
		}
		return rows, idx
	case viewDiff:
		if fc.status == "" || fc.status == "A" || fc.status == "D" {
			return cut(fc, viewFile, 0, 0)
		}
		const near = 3
		keep := make([]bool, len(all))
		for i, r := range all {
			if r.kind == rowAdd || r.kind == rowDel {
				for j := max(0, i-near); j <= min(len(all)-1, i+near); j++ {
					keep[j] = true
				}
			}
		}
		last := -1
		for i := range all {
			if !keep[i] {
				continue
			}
			if i != last+1 {
				rows, idx = append(rows, row{kind: rowGap, text: strconv.Itoa(i - last - 1)}), append(idx, -1)
			}
			rows, idx = append(rows, all[i]), append(idx, i)
			last = i
		}
		if last >= 0 && last < len(all)-1 {
			rows, idx = append(rows, row{kind: rowGap, text: strconv.Itoa(len(all) - 1 - last)}), append(idx, -1)
		}
		return rows, idx
	}
	idx = make([]int, len(all))
	for i := range all {
		idx[i] = i
	}
	return all, idx
}

// nearestNew is the new-side line a deleted row sits beside.
func nearestNew(rows []row, i int) int {
	for j := i + 1; j < len(rows); j++ {
		if rows[j].kind != rowDel {
			return rows[j].new
		}
	}
	for j := i - 1; j >= 0; j-- {
		if rows[j].kind != rowDel {
			return rows[j].new
		}
	}
	return 0
}

// rowFor is the index in rows showing line, or the nearest one after it.
func rowFor(rows []row, line int) int {
	best := -1
	for i, r := range rows {
		n := r.new
		if r.kind == rowDel && r.new == 0 && !hasNew(rows) {
			n = r.old // a deleted file has only old lines
		}
		if r.kind == rowGap || n == 0 {
			continue
		}
		if n == line {
			return i
		}
		if n > line && best < 0 {
			best = i
		}
	}
	if best < 0 {
		return max(0, len(rows)-1)
	}
	return best
}

func hasNew(rows []row) bool {
	for _, r := range rows {
		if r.new > 0 {
			return true
		}
	}
	return false
}

// firstChange is the first added or deleted row, or 0.
func firstChange(rows []row) int {
	for i, r := range rows {
		if r.kind == rowAdd || r.kind == rowDel {
			return i
		}
	}
	return 0
}

// anchor is where a note on this row goes, in GitHub's terms.
func (r row) anchor() (int, string) {
	switch r.kind {
	case rowDel:
		return r.old, "LEFT"
	case rowGap:
		return 0, ""
	}
	return r.new, "RIGHT"
}

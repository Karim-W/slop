package ui

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/karim-w/slop/apps/gitmon/internal/git"
)

// The working tree box lists every changed path once, either flat or as a
// directory tree. Staging does not move a row anywhere: a staged row is
// highlighted in place, and a folder row stages, unstages or discards every
// change beneath it.

type treeKind int

const (
	rowDir treeKind = iota
	rowFile
)

type stageState int

const (
	stageNone stageState = iota
	stagePartial
	stageFull
)

// change merges the index and worktree sides of one path.
type change struct {
	path string
	st   *git.FileStatus // staged side
	un   *git.FileStatus // unstaged / untracked side
}

func (c change) state() stageState {
	switch {
	case c.un == nil:
		return stageFull
	case c.st == nil:
		return stageNone
	}
	return stagePartial
}

// rep is the side shown and acted on: what is left to stage, else what is
// staged.
func (c change) rep() git.FileStatus {
	if c.un != nil {
		return *c.un
	}
	return *c.st
}

type treeRow struct {
	kind    treeKind
	path    string // directory path or file path
	label   string // display name; compacted single-child chains read "a/b/c"
	depth   int
	changes []change // the file itself, or every change beneath the folder
	state   stageState
}

func (r treeRow) key() string { return fmt.Sprintf("%d:%s", r.kind, r.path) }

func (r treeRow) file() git.FileStatus { return r.changes[0].rep() }

func (r treeRow) staged() []git.FileStatus {
	var out []git.FileStatus
	for _, c := range r.changes {
		if c.st != nil {
			out = append(out, *c.st)
		}
	}
	return out
}

func (r treeRow) unstaged() []git.FileStatus {
	var out []git.FileStatus
	for _, c := range r.changes {
		if c.un != nil {
			out = append(out, *c.un)
		}
	}
	return out
}

// paths returns every path the row covers on both sides, including rename
// sources, so unstaging a rename also restores its old path.
func (r treeRow) paths() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, c := range r.changes {
		add(c.path)
		for _, f := range []*git.FileStatus{c.st, c.un} {
			if f != nil {
				add(f.OrigPath)
			}
		}
	}
	return out
}

func stateOf(cs []change) stageState {
	full, none := true, true
	for _, c := range cs {
		switch c.state() {
		case stageFull:
			none = false
		case stageNone:
			full = false
		default:
			full, none = false, false
		}
	}
	switch {
	case full:
		return stageFull
	case none:
		return stageNone
	}
	return stagePartial
}

func (m *Model) changes() []change {
	byPath := map[string]*change{}
	var order []string
	get := func(p string) *change {
		c := byPath[p]
		if c == nil {
			c = &change{path: p}
			byPath[p] = c
			order = append(order, p)
		}
		return c
	}
	for i := range m.status.Staged {
		f := &m.status.Staged[i]
		get(f.Path).st = f
	}
	for i := range m.status.Unstaged {
		f := &m.status.Unstaged[i]
		get(f.Path).un = f
	}
	sort.Strings(order)
	out := make([]change, len(order))
	for i, p := range order {
		out[i] = *byPath[p]
	}
	return out
}

// rebuildTree recomputes the visible rows. Selection is kept by row identity
// where possible.
func (m *Model) rebuildTree() {
	prev, had := m.selectedRow()
	m.tree = m.tree[:0]
	cs := m.changes()
	if m.flatTree {
		for _, c := range cs {
			m.tree = append(m.tree, treeRow{kind: rowFile, path: c.path, label: c.path, changes: []change{c}, state: c.state()})
		}
	} else {
		root := &dirNode{}
		for _, c := range cs {
			root.insert(c)
		}
		m.emitDir(root, "", 0)
	}
	if had {
		if i := m.findRow(prev.key()); i >= 0 {
			m.ch.sel = i
			return
		}
		// the row vanished (folder collapsed, view switched, file committed):
		// land on the folder that holds it or the first file it held
		best := -1
		for i, r := range m.tree {
			if r.kind == rowDir && strings.HasPrefix(prev.path, r.path+"/") {
				best = i
			}
			if prev.kind == rowDir && strings.HasPrefix(r.path, prev.path+"/") {
				best = i
				break
			}
		}
		if best >= 0 {
			m.ch.sel = best
			return
		}
	}
	m.ch.sel = clamp(m.ch.sel, 0, max(0, len(m.tree)-1))
}

func (m *Model) findRow(key string) int {
	for i, r := range m.tree {
		if r.key() == key {
			return i
		}
	}
	return -1
}

type dirNode struct {
	dirs  map[string]*dirNode
	files []change
}

func (n *dirNode) insert(c change) {
	parts := strings.Split(c.path, "/")
	cur := n
	for _, p := range parts[:len(parts)-1] {
		if cur.dirs == nil {
			cur.dirs = map[string]*dirNode{}
		}
		next := cur.dirs[p]
		if next == nil {
			next = &dirNode{}
			cur.dirs[p] = next
		}
		cur = next
	}
	cur.files = append(cur.files, c)
}

func (n *dirNode) all() []change {
	out := append([]change{}, n.files...)
	for _, name := range n.names() {
		out = append(out, n.dirs[name].all()...)
	}
	return out
}

func (n *dirNode) names() []string {
	names := make([]string, 0, len(n.dirs))
	for k := range n.dirs {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// emitDir writes folders first, then files, VS Code style. A chain of
// folders with a single child and no files is compacted onto one row.
func (m *Model) emitDir(n *dirNode, prefix string, depth int) {
	for _, name := range n.names() {
		child := n.dirs[name]
		label := name
		for len(child.files) == 0 && len(child.dirs) == 1 {
			only := child.names()[0]
			label += "/" + only
			child = child.dirs[only]
		}
		p := path.Join(prefix, label)
		all := child.all()
		m.tree = append(m.tree, treeRow{kind: rowDir, path: p, label: label, depth: depth, changes: all, state: stateOf(all)})
		if !m.collapsed[p] {
			m.emitDir(child, p, depth+1)
		}
	}
	for _, c := range n.files {
		m.tree = append(m.tree, treeRow{kind: rowFile, path: c.path, label: path.Base(c.path), depth: depth, changes: []change{c}, state: c.state()})
	}
}

// ─── Selection ──────────────────────────────────────────────────────────────

func (m *Model) itemCount() int { return len(m.tree) }

// changeLine maps an item to its line in the tree list (one line per row).
func (m *Model) changeLine(i int) int { return i }

func (m *Model) treeLines() int { return len(m.tree) }

func (m *Model) selectedRow() (treeRow, bool) {
	if m.ch.sel >= 0 && m.ch.sel < len(m.tree) {
		return m.tree[m.ch.sel], true
	}
	return treeRow{}, false
}

// selectedChange returns the selected file's active side; folders return
// false.
func (m *Model) selectedChange() (git.FileStatus, bool) {
	r, ok := m.selectedRow()
	if !ok || r.kind != rowFile {
		return git.FileStatus{}, false
	}
	return r.file(), true
}

// parentRow finds the folder row that contains the selected row.
func (m *Model) parentRow() int {
	r, ok := m.selectedRow()
	if !ok {
		return -1
	}
	for i := m.ch.sel - 1; i >= 0; i-- {
		if p := m.tree[i]; p.kind == rowDir && p.depth < r.depth {
			return i
		}
	}
	return -1
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

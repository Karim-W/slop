package ui

import (
	"strings"

	"github.com/karim-w/slop/apps/slash/internal/vfs"
	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/utils/humanize"
	"github.com/karim-w/slop/pkg/utils/textx"
)

type side int

const (
	localSide side = iota
	remoteSide
)

const parentEntryName = ".."

type pane struct {
	side       side
	fs         vfs.FileSystem
	path       string
	entries    []vfs.Entry
	visible    []vfs.Entry
	marked     map[string]bool
	cursor     int
	scroll     tui.Scroll
	showHidden bool
	filter     string
	loading    bool
	err        error
}

func newPane(s side) pane {
	return pane{side: s, marked: map[string]bool{}}
}

func (p *pane) connected() bool { return p.fs != nil }

func (p *pane) title() string {
	if p.fs == nil {
		return "Remote"
	}
	return p.fs.Label()
}

// setListing replaces the directory contents, trying to keep the cursor on
// focusName so refreshes and renames don't make the selection jump.
func (p *pane) setListing(path string, entries []vfs.Entry, focusName string) {
	if path != p.path {
		p.marked = map[string]bool{}
		p.filter = ""
	}
	p.path = path
	p.entries = entries
	p.err = nil
	p.loading = false
	p.rebuildVisible()
	p.cursor = 0
	for i, entry := range p.visible {
		if entry.Name == focusName {
			p.cursor = i
			break
		}
	}
}

func (p *pane) rebuildVisible() {
	p.visible = p.visible[:0]
	if p.fs != nil && p.fs.Parent(p.path) != p.path {
		p.visible = append(p.visible, vfs.Entry{Name: parentEntryName, IsDir: true})
	}
	filter := strings.ToLower(p.filter)
	for _, entry := range p.entries {
		if !p.showHidden && vfs.IsHidden(entry.Name) {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(entry.Name), filter) {
			continue
		}
		p.visible = append(p.visible, entry)
	}
	p.cursor = min(p.cursor, max(len(p.visible)-1, 0))
}

func (p *pane) current() (vfs.Entry, bool) {
	if p.cursor < 0 || p.cursor >= len(p.visible) {
		return vfs.Entry{}, false
	}
	return p.visible[p.cursor], true
}

func (p *pane) currentName() string {
	entry, _ := p.current()
	return entry.Name
}

func (p *pane) move(delta int) {
	p.cursor = min(max(p.cursor+delta, 0), max(len(p.visible)-1, 0))
}

func (p *pane) toggleMark() {
	entry, ok := p.current()
	if !ok || entry.Name == parentEntryName {
		return
	}
	if p.marked[entry.Name] {
		delete(p.marked, entry.Name)
	} else {
		p.marked[entry.Name] = true
	}
}

func (p *pane) toggleMarkAll() {
	if len(p.marked) > 0 {
		p.marked = map[string]bool{}
		return
	}
	for _, entry := range p.visible {
		if entry.Name != parentEntryName {
			p.marked[entry.Name] = true
		}
	}
}

// selection is what an action applies to: the marked entries, or the entry
// under the cursor when nothing is marked.
func (p *pane) selection() []vfs.Entry {
	var selected []vfs.Entry
	for _, entry := range p.visible {
		if p.marked[entry.Name] {
			selected = append(selected, entry)
		}
	}
	if len(selected) > 0 {
		return selected
	}
	if entry, ok := p.current(); ok && entry.Name != parentEntryName {
		return []vfs.Entry{entry}
	}
	return nil
}

func (p *pane) has(name string) bool {
	for _, entry := range p.entries {
		if entry.Name == name {
			return true
		}
	}
	return false
}

// view is the pane as a panel: title and path in the top border, a status
// label on the right, and the directory listing inside.
func (p *pane) view(focused bool) tui.View {
	panel := tui.Panel(p.body(focused)).Focused(focused).Label(p.statusLabel()).Grow()
	if p.fs == nil {
		return panel.Title(p.title())
	}
	return panel.Title(p.title()).Subtitle(p.path)
}

func (p *pane) statusLabel() tui.View {
	switch {
	case p.fs == nil:
		return nil
	case p.loading:
		return tui.Text("loading…").Fg(tui.Muted)
	case len(p.marked) > 0:
		return tui.Text(textx.Pluralize(len(p.marked), "marked", "marked")).Fg(tui.Warning)
	case p.filter != "":
		return tui.Text("filter: " + p.filter).Fg(tui.Warning)
	}
	return tui.Text(textx.Pluralize(len(p.entries), "item", "items")).Fg(tui.Muted)
}

func (p *pane) body(focused bool) tui.View {
	switch {
	case p.fs == nil:
		return tui.Center(tui.VStack(
			tui.Text("Not connected").Bold().Align(tui.Middle),
			tui.Text(""),
			tui.Line(
				tui.S("Press ", tui.Fg(tui.Muted)),
				tui.S("s", tui.Fg(tui.Accent).Bold()),
				tui.S(" to open the site manager", tui.Fg(tui.Muted)),
			),
		))
	case p.err != nil:
		return tui.Pad(tui.Text(p.err.Error()).Fg(tui.Danger).Wrap(), 1, 1, 0, 1)
	}
	cursorBg := tui.Mix(tui.Background, tui.Selection, 0.6)
	if focused {
		cursorBg = tui.Selection
	}
	// Columns depend on the width the pane ends up with, so build rows lazily.
	return tui.Lazy(func(size tui.Size) tui.View {
		showTime := size.W >= 52
		return tui.List(len(p.visible), p.cursor, &p.scroll, func(i int, _ bool) tui.View {
			return p.row(p.visible[i], showTime)
		}).Highlight(cursorBg).Empty(tui.Pad(tui.Text("empty").Fg(tui.Muted), 1, 1))
	})
}

func (p *pane) row(entry vfs.Entry, showTime bool) tui.View {
	marker := tui.Text("  ")
	name := entry.Name
	color := tui.Color(tui.Primary)
	bold := false
	switch {
	case p.marked[entry.Name]:
		marker = tui.Text("● ").Fg(tui.Warning)
		color = tui.Warning
	case entry.IsDir:
		color, bold = tui.Accent, true
	case entry.IsLink:
		color = tui.Accent2
	}
	if entry.IsDir && entry.Name != parentEntryName {
		name += "/"
	}
	nameText := tui.Text(name).Fg(color)
	if bold {
		nameText = nameText.Bold()
	}
	if entry.IsLink {
		nameText = tui.Text(name + " →").Fg(color)
	}
	size := ""
	if !entry.IsDir {
		size = humanize.Bytes(entry.Size)
	}
	return tui.HStack(
		tui.Text(" "),
		marker,
		nameText.Grow(),
		tui.Width(10, tui.Text(size).Fg(tui.Muted).Align(tui.End)),
		tui.If(showTime, tui.Width(14, tui.Text(humanize.ModTime(entry.ModTime)).Fg(tui.Muted).Align(tui.End))),
	)
}

package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/karim-w/slop/slash/internal/vfs"
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
	offset     int
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

func (p *pane) view(width, height int, focused bool) string {
	innerWidth := width - 2
	rows := height - 2
	label := p.statusLabel()

	if p.fs == nil {
		return frame(p.title(), "", disconnectedBody(innerWidth, rows), width, height, focused)
	}

	pathBudget := innerWidth - lipgloss.Width(p.title()) - lipgloss.Width(label) - 10
	pathTitle := styleBold.Render(p.title()) + styleMuted.Render("  "+truncateMiddle(p.path, max(pathBudget, 10)))
	body := p.bodyLines(innerWidth, rows, focused)
	return frame(pathTitle, label, body, width, height, focused)
}

func (p *pane) statusLabel() string {
	switch {
	case p.loading:
		return styleMuted.Render("loading…")
	case len(p.marked) > 0:
		return styleMarked.Render(pluralize(len(p.marked), "marked", "marked"))
	case p.filter != "":
		return styleWarn.Render("filter: " + p.filter)
	default:
		return styleMuted.Render(pluralize(len(p.entries), "item", "items"))
	}
}

func (p *pane) bodyLines(width, rows int, focused bool) []string {
	if p.err != nil {
		return []string{"", " " + styleError.Render(p.err.Error())}
	}
	if len(p.visible) == 0 {
		return []string{"", styleMuted.Render(" empty")}
	}

	p.scrollIntoView(rows)
	lines := make([]string, 0, rows)
	for i := p.offset; i < len(p.visible) && len(lines) < rows; i++ {
		lines = append(lines, p.row(p.visible[i], width, i == p.cursor, focused))
	}
	return lines
}

func (p *pane) scrollIntoView(rows int) {
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+rows {
		p.offset = p.cursor - rows + 1
	}
	p.offset = min(p.offset, max(len(p.visible)-rows, 0))
}

func (p *pane) row(entry vfs.Entry, width int, underCursor, focused bool) string {
	const sizeWidth, timeWidth = 10, 14
	showTime := width >= 52

	nameWidth := width - 3 - sizeWidth
	if showTime {
		nameWidth -= timeWidth
	}

	marker, name := "  ", entry.Name
	nameStyle := styleText
	switch {
	case p.marked[entry.Name]:
		marker = styleMarked.Render("● ")
		nameStyle = styleMarked
	case entry.IsDir:
		nameStyle = styleDir
	case entry.IsLink:
		nameStyle = styleLink
	}
	if entry.IsDir && entry.Name != parentEntryName {
		name += "/"
	}
	if entry.IsLink {
		name += styleMuted.Render(" →")
	}

	size := ""
	if !entry.IsDir {
		size = formatBytes(entry.Size)
	}

	line := " " + marker + fit(nameStyle.Render(name), nameWidth) + styleMuted.Render(fitLeft(size, sizeWidth))
	if showTime {
		line += styleMuted.Render(fitLeft(formatModTime(entry.ModTime), timeWidth))
	}

	if !underCursor {
		return line
	}
	background := colorCursorDim
	if focused {
		background = colorCursor
	}
	return highlight(line, width, background)
}

func highlight(line string, width int, background lipgloss.TerminalColor) string {
	style := lipgloss.NewStyle().Background(background)
	padded := fit(line, width)
	return style.Render(strings.ReplaceAll(padded, "\x1b[0m", "\x1b[0m"+backgroundSequence(style)))
}

// backgroundSequence extracts the escape code that turns on style's
// background, so highlighted rows keep it after inner styles reset.
func backgroundSequence(style lipgloss.Style) string {
	rendered := style.Render(" ")
	if i := strings.Index(rendered, " "); i > 0 {
		return rendered[:i]
	}
	return ""
}

func disconnectedBody(width, rows int) []string {
	message := []string{
		styleBold.Render("Not connected"),
		"",
		styleMuted.Render("Press ") + styleKey.Render("s") + styleMuted.Render(" to open the site manager"),
	}
	lines := make([]string, max(rows/2-2, 0))
	for _, line := range message {
		pad := max((width-lipgloss.Width(line))/2, 0)
		lines = append(lines, strings.Repeat(" ", pad)+line)
	}
	return lines
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + plural
}

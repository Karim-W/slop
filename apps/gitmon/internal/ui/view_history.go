package ui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/karim-w/slop/apps/gitmon/internal/git"
	"github.com/karim-w/slop/apps/gitmon/internal/graph"
	"github.com/karim-w/slop/pkg/domain/theme"
	"github.com/karim-w/slop/pkg/tui"
)

// histGeom describes where things sit inside the history box.
type histGeom struct {
	in       rect
	cw       int // terminal cells per commit column
	vis      int // commit columns visible
	lanes    int // lane rows available
	shown    int // lane rows drawn
	lanesMax int // widest lane count in view
	top      int // blank rows above the label row (vertical centring)
	xoff     int // first visible position
	n        int
	laneY0   int // screen row of the first lane
}

func (m *Model) histGeom(r rect) histGeom {
	in := r.inner()
	g := histGeom{in: in, n: len(m.commits)}
	g.cw = 3
	if g.n > 0 {
		g.cw = clamp((in.w-2)/g.n, 3, 6)
	}
	g.vis = max(1, (in.w-2)/g.cw)
	g.lanes = max(1, in.h-2)
	g.xoff = m.gx.Offset(g.vis, g.n)
	for p := g.xoff; p < min(g.n, g.xoff+g.vis); p++ {
		if i := g.n - 1 - p; i < len(m.rows) {
			g.lanesMax = max(g.lanesMax, len(m.rows[i].Cells))
		}
	}
	g.shown = min(g.lanes, max(1, g.lanesMax))
	g.top = max(0, (g.lanes-g.shown)/2)
	g.laneY0 = in.y + 1 + g.top
	return g
}

// pos converts a commit index (0 = newest) to a left-to-right position.
func (m *Model) pos(i int) int { return len(m.commits) - 1 - i }

func (g histGeom) nodeX(p int) int { return g.in.x + 1 + (p-g.xoff)*g.cw }

func (m *Model) followGraph(snap bool) {
	if !m.ready || len(m.commits) == 0 {
		return
	}
	_, _, hr := m.layout()
	g := m.histGeom(hr)
	m.gx.Follow(m.pos(m.sel), g.vis, g.n)
	if snap {
		m.gx.Snap(m.gx.Target)
	}
	if m.sel < len(m.rows) {
		lane := m.rows[m.sel].NodeCol
		if lane < m.laneOff {
			m.laneOff = lane
		}
		if lane >= m.laneOff+g.shown {
			m.laneOff = lane - g.shown + 1
		}
	}
}

func (m *Model) nodeGlyph(c git.Commit) string {
	switch {
	case c.WIP:
		return "◌"
	case c.IsHead:
		return "◉"
	case len(c.Parents) > 1:
		return "◆"
	}
	return "●"
}

// cellLine is a row of single-width styled cells.
type cellLine []string

func newCellLine(w int) cellLine {
	l := make(cellLine, w)
	for i := range l {
		l[i] = " "
	}
	return l
}

func (l cellLine) put(x int, text string, st lipgloss.Style) int {
	for _, r := range text {
		if x >= 0 && x < len(l) {
			l[x] = st.Render(string(r))
		}
		x++
	}
	return x
}

func (l cellLine) String() string { return strings.Join(l, "") }

func (m *Model) historyPanel(r rect) tui.View {
	focused := m.focus == boxGraph
	panel := tui.Panel(nil).Title("History").Focused(focused).Hints(
		tui.H("←→", "move"), tui.H("↑↓", "lane"), tui.H("⏎", "open"), tui.H("space", "checkout"),
		tui.H("/", "search"), tui.H("F", "focus"), tui.H("b", "branch"), tui.H("x", "reset"))
	switch {
	case !m.loaded:
		return panel.Body(emptyState(tui.Gradient("reading history…")))
	case m.loadErr != nil:
		return panel.Body(tui.Text(" " + m.loadErr.Error()).Fg(tui.Danger))
	case len(m.commits) == 0:
		return panel.Body(emptyState(tui.Gradient("no commits yet — make history", tui.Accent, tui.Accent2)))
	}

	g := m.histGeom(r)
	label := []tui.Span{tui.S(fmt.Sprintf("%d / %d", m.pos(m.sel)+1, g.n), tui.Fg(tui.Muted))}
	if g.lanesMax > g.shown {
		label = append([]tui.Span{tui.S(fmt.Sprintf("lanes %d–%d of %d · ", m.laneOff+1, m.laneOff+g.shown, g.lanesMax), tui.Fg(tui.Muted))}, label...)
	}
	if g.xoff > 0 {
		label = append([]tui.Span{tui.S("◂ ", tui.Fg(tui.Accent))}, label...)
	}
	graph := tui.Func(func(c tui.Ctx, _ tui.Size) []string { return m.historyLines(c.P, g, focused) })
	return panel.Label(tui.Line(label...)).Body(graph)
}

// historyLines draws the commit graph: ref labels on top, one row per lane,
// and a date axis at the bottom.
func (m *Model) historyLines(s *Styles, g histGeom, focused bool) []string {
	in := g.in
	p0, p1 := g.xoff, min(g.n, g.xoff+g.vis)
	selP := m.pos(m.sel)
	m.laneOff = clamp(m.laneOff, 0, max(0, g.lanesMax-g.shown))
	colW := g.cw
	dim := s
	if m.focusSet != nil {
		dim = s.Dimmed(0.62)
	}

	// lanes
	lines := make([]cellLine, g.shown)
	for row := 0; row < g.shown; row++ {
		j := m.laneOff + row
		cl := newCellLine(in.w)
		for p := p0; p < p1; p++ {
			i := g.n - 1 - p
			st := s
			if m.focusSet != nil && !m.focusSet[i] {
				st = dim
			}
			x := 1 + (p-g.xoff)*colW
			R := m.rows[i]
			var selBg color.Color
			if p == selP && focused {
				selBg = st.Sel
			}
			if j < len(R.Cells) {
				c := R.Cells[j]
				if c.Glyph == graph.Node {
					c2 := m.commits[i]
					lc := st.Lane(R.Color)
					if c2.WIP {
						lc = st.Warning
					}
					nst := on(lc, selBg).Bold(true)
					if p == selP {
						nst = lipgloss.NewStyle().Foreground(st.Bg).Background(lc).Bold(true)
					} else if m.query != "" && m.matchCommit(c2) {
						nst = on(st.Warning, selBg).Bold(true)
					}
					cl.put(x, m.nodeGlyph(c2), nst)
				} else if c.Glyph != ' ' {
					cl.put(x, string(graph.Horizontal(c.Glyph)), on(st.Lane(c.Color), selBg))
				} else if selBg != nil {
					cl.put(x, " ", on(st.Text, selBg))
				}
			} else if selBg != nil {
				cl.put(x, " ", on(st.Text, selBg))
			}
			// connector toward the next (newer) commit
			if i > 0 {
				nr := m.rows[i-1]
				if j < len(nr.Below) && nr.Below[j] {
					nst := s
					if m.focusSet != nil && (!m.focusSet[i] || !m.focusSet[i-1]) {
						nst = dim
					}
					cl.put(x+1, strings.Repeat("─", colW-1), fg(nst.Lane(nr.BelowColor[j])))
				}
			}
		}
		lines[row] = cl
	}

	// ref labels (top row)
	labels := newCellLine(in.w)
	if m.searching || m.query != "" {
		var q string
		if m.searching {
			m.search.SetWidth(max(10, in.w-30))
			q = " " + m.search.View()
		} else {
			q = " " + fg(s.Accent2).Render("/ ") + fg(s.Text).Render(m.query) + fg(s.Muted).Render(fmt.Sprintf("  %d matches · n/N · esc clear", len(m.matches())))
		}
		labels = cellLine{pad(q, in.w)}
	} else if m.focusMode {
		labels = cellLine{pad(" "+chip("◎ FOCUS", s.Bg, s.Accent2, true)+fg(s.Muted).Render(" lineage of "+m.selectedLabel()+" · F to exit"), in.w)}
	} else {
		m.placeLabels(s, labels, g, p0, p1)
	}

	// date axis (bottom row)
	axis := newCellLine(in.w)
	free, lastDay := 0, ""
	for p := p0; p < p1; p++ {
		c := m.commits[g.n-1-p]
		day := c.Time.Format("2006-01-02")
		x := 1 + (p-g.xoff)*g.cw
		if day == lastDay || x < free {
			continue
		}
		label := c.Time.Format("Jan 2")
		if c.Time.Year() != time.Now().Year() {
			label = c.Time.Format("Jan 2 '06")
		}
		axis.put(x, "╵"+label, fg(s.Muted))
		free = x + len(label) + 3
		lastDay = day
	}
	axis.put(1+(selP-g.xoff)*colW, "▲", fg(s.Accent2).Bold(true))

	var out []string
	for i := 0; i < g.top; i++ {
		out = append(out, "")
	}
	out = append(out, labels.String())
	for _, l := range lines {
		out = append(out, l.String())
	}
	for len(out) < in.h-1 {
		out = append(out, "")
	}
	return append(out, axis.String())
}

func (m *Model) selectedLabel() string {
	c := m.selectedCommit()
	if c.WIP {
		return "working tree"
	}
	return c.Short
}

func (m *Model) placeLabels(s *Styles, l cellLine, g histGeom, p0, p1 int) {
	used := make([]bool, len(l))
	place := func(p int) {
		i := g.n - 1 - p
		c := m.commits[i]
		if len(c.Refs) == 0 {
			return
		}
		text, st := primaryRef(s, c, s.Lane(m.rows[i].Color))
		x := 1 + (p-g.xoff)*g.cw
		w := ansi.StringWidth(text)
		if x+w > len(l) {
			x = len(l) - w
		}
		if x < 0 {
			return
		}
		for k := max(0, x-1); k < min(len(l), x+w+1); k++ {
			if used[k] {
				return
			}
		}
		l.put(x, text, st)
		for k := x; k < min(len(l), x+w); k++ {
			used[k] = true
		}
	}
	// HEAD and the selection first, then everything else
	for p := p0; p < p1; p++ {
		if m.commits[g.n-1-p].IsHead {
			place(p)
		}
	}
	for p := p1 - 1; p >= p0; p-- {
		place(p)
	}
}

// primaryRef returns the most important ref label of a commit.
func primaryRef(s *Styles, c git.Commit, lane color.Color) (string, lipgloss.Style) {
	extra := ""
	if len(c.Refs) > 1 {
		extra = fmt.Sprintf(" +%d", len(c.Refs)-1)
	}
	tint := theme.Mix(s.Bg, lane, 0.22)
	for _, kind := range []git.RefKind{git.RefHead, git.RefLocal, git.RefTag, git.RefRemote} {
		for _, r := range c.Refs {
			if r.Kind != kind {
				continue
			}
			name := truncate(r.Name, 22)
			switch kind {
			case git.RefHead:
				return " ◉ " + name + extra + " ", lipgloss.NewStyle().Foreground(s.Bg).Background(lane).Bold(true)
			case git.RefLocal:
				return " " + name + extra + " ", lipgloss.NewStyle().Foreground(lane).Background(tint).Bold(true)
			case git.RefTag:
				return " ◈ " + name + extra + " ", lipgloss.NewStyle().Foreground(s.Warning).Background(theme.Mix(s.Bg, s.Warning, 0.18))
			case git.RefRemote:
				return "☁ " + name + extra, fg(lane).Italic(true)
			}
		}
	}
	return "", lipgloss.NewStyle()
}

// ─── Tooltip ────────────────────────────────────────────────────────────────

// tooltip floats a card about the selected commit over the History box's top
// edge, with a notch pointing at the commit.
func (m *Model) tooltip(hr rect) (tui.Layer, bool) {
	if len(m.commits) == 0 || !m.loaded {
		return tui.Layer{}, false
	}
	g := m.histGeom(hr)
	p := m.pos(m.sel)
	if p < g.xoff || p >= g.xoff+g.vis {
		return tui.Layer{}, false
	}
	lane := m.rows[m.sel].NodeCol
	if lane < m.laneOff || lane >= m.laneOff+g.shown {
		return tui.Layer{}, false
	}
	nx := g.nodeX(p)
	ny := g.laneY0 + (lane - m.laneOff)
	c := m.selectedCommit()
	const tw = 46
	th := len(m.tooltipContent(m.st, c, tw-4)) + 2
	// Dock the tooltip on the History box's top edge so it never hides lanes;
	// the notch points down at the selected column.
	above := hr.y-th+1 >= 1
	x0 := clamp(nx-6, 0, m.w-tw)
	y0 := ny + 1
	if above {
		y0 = hr.y - th + 1
	}
	card := tui.Func(func(ctx tui.Ctx, size tui.Size) []string {
		return m.tooltipLines(ctx.P, c, tw, nx-x0, above)
	})
	return tui.At(tui.Start, tui.Start, tui.Frame(tw, th, card)).Offset(x0, y0), true
}

// tooltipLines draws the card: a border in the commit's lane colour around
// its content, with the notch on the edge facing the commit.
// tooltipLines draws the card: a border in the commit's lane colour around
// its content, with the notch on the edge facing the commit.
func (m *Model) tooltipLines(s *Styles, c git.Commit, tw, notch int, above bool) []string {
	iw := tw - 4
	bc := fg(m.tooltipColor(s, c))
	top := "╭" + strings.Repeat("─", tw-2) + "╮"
	bot := "╰" + strings.Repeat("─", tw-2) + "╯"
	if notch > 0 && notch < tw-1 {
		if above {
			bot = "╰" + strings.Repeat("─", notch-1) + "┬" + strings.Repeat("─", tw-2-notch) + "╯"
		} else {
			top = "╭" + strings.Repeat("─", notch-1) + "┴" + strings.Repeat("─", tw-2-notch) + "╮"
		}
	}
	out := []string{bc.Render(top)}
	for _, l := range m.tooltipContent(s, c, iw) {
		out = append(out, bc.Render("│")+" "+pad(l, iw)+" "+bc.Render("│"))
	}
	return append(out, bc.Render(bot))
}

func (m *Model) tooltipColor(s *Styles, c git.Commit) color.Color {
	if c.WIP {
		return s.Warning
	}
	return s.Lane(m.rows[m.sel].Color)
}

// tooltipContent is the card's lines, iw cells wide.
func (m *Model) tooltipContent(s *Styles, c git.Commit, iw int) []string {
	if c.WIP {
		return []string{
			fg(s.Warning).Bold(true).Render("◌ Uncommitted changes"),
			fg(s.Success).Render(fmt.Sprintf("● %d staged", len(m.status.Staged))) + "  " + fg(s.Modified).Render(fmt.Sprintf("◐ %d unstaged", len(m.status.Unstaged))),
			fg(s.Muted).Render("⏎ jump to working tree"),
		}
	}
	lc := m.tooltipColor(s, c)
	who := avatar(s, c.Author, nil) + " " + fg(s.Text).Bold(true).Render(truncate(c.Author, iw-14))
	L := []string{
		spread(who, fg(s.Sub).Render(relTimeLong(c.Time)), iw),
		spread(fg(s.Muted).Render(c.Time.Format("Mon Jan 2 2006 · 15:04")), chip(c.Short, s.Bg, lc, true), iw),
	}
	if refs := refChipsInline(s, c, lc); refs != "" {
		L = append(L, truncate(refs, iw))
	}
	L = append(L, fg(s.Text).Render(truncate(c.Subject, iw)))
	if len(c.Parents) > 1 {
		L = append(L, fg(s.Muted).Render(fmt.Sprintf("merge of %d parents", len(c.Parents))))
	}
	return L
}

func spread(l, r string, w int) string {
	gap := w - ansi.StringWidth(l) - ansi.StringWidth(r)
	if gap < 1 {
		return truncate(l+" "+r, w)
	}
	return l + strings.Repeat(" ", gap) + r
}

func refChipsInline(s *Styles, c git.Commit, lane color.Color) string {
	var parts []string
	tint := theme.Mix(s.Bg, lane, 0.22)
	for _, r := range c.Refs {
		switch r.Kind {
		case git.RefHead:
			parts = append(parts, chip("◉ "+r.Name, s.Bg, lane, true))
		case git.RefLocal:
			parts = append(parts, chip(r.Name, lane, tint, true))
		case git.RefTag:
			parts = append(parts, chip("◈ "+r.Name, s.Warning, theme.Mix(s.Bg, s.Warning, 0.18), false))
		case git.RefRemote:
			parts = append(parts, fg(lane).Italic(true).Render("☁ "+r.Name))
		}
	}
	return strings.Join(parts, " ")
}

// ─── Search ─────────────────────────────────────────────────────────────────

func (m *Model) matchCommit(c git.Commit) bool {
	q := strings.ToLower(m.query)
	if q == "" || c.WIP {
		return false
	}
	if strings.HasPrefix(c.Hash, q) || strings.Contains(strings.ToLower(c.Subject), q) || strings.Contains(strings.ToLower(c.Author), q) {
		return true
	}
	for _, r := range c.Refs {
		if strings.Contains(strings.ToLower(r.Name), q) {
			return true
		}
	}
	return false
}

func (m *Model) matches() []int {
	var out []int
	for i, c := range m.commits {
		if m.matchCommit(c) {
			out = append(out, i)
		}
	}
	return out
}

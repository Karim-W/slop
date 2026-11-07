package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/karim-w/slop/pkg/utils/textx"
)

// PanelView is the design language's container: a rounded box with a title
// set into the top border, an optional label on the right, and key hints in
// the bottom border while focused. A focused panel's border is a gradient
// across the three accent colours; an idle one is drawn in Border.
//
//	╭─ ● Files  ~/src ───────── 12 items ─╮
//	│ …                                   │
//	╰─ enter open · space mark ───────────╯
type PanelView struct {
	body     View
	title    string
	subtitle string
	label    View
	hints    []Hint
	focused  bool
	border   Color
	weight   int
}

// Panel wraps body in a box.
func Panel(body View) PanelView { return PanelView{body: body} }

// Body replaces the panel's content.
func (p PanelView) Body(v View) PanelView { p.body = v; return p }

// Title is shown at the top left, bold.
func (p PanelView) Title(s string) PanelView { p.title = s; return p }

// Subtitle follows the title in Muted, such as a path.
func (p PanelView) Subtitle(s string) PanelView { p.subtitle = s; return p }

// Label is shown at the top right, such as a count or status.
func (p PanelView) Label(v View) PanelView { p.label = v; return p }

// Hints are shown in the bottom border while the panel is focused.
func (p PanelView) Hints(h ...Hint) PanelView { p.hints = h; return p }

// Focused switches to the accent gradient border.
func (p PanelView) Focused(f bool) PanelView { p.focused = f; return p }

// BorderColor overrides the border colour, such as Danger for a warning.
func (p PanelView) BorderColor(c Color) PanelView { p.border = c; return p }

// Grow makes the panel take leftover space in a stack.
func (p PanelView) Grow() PanelView { p.weight = 1; return p }

func (p PanelView) flexWeight() int { return p.weight }

func (p PanelView) Measure(c Ctx, avail Size) Size {
	z := Measure(c, p.body, Size{avail.W - 2, avail.H - 2})
	tw := textx.Width(p.title) + textx.Width(p.subtitle) + 8
	return Size{min(max(z.W+2, tw), avail.W), min(z.H+2, avail.H)}
}

// edge returns the border colour of each column.
func (p PanelView) edge(c Ctx, w int) []color.Color {
	if p.border != nil {
		return solid(resolve(p.border, c.P), w)
	}
	if p.focused {
		return lipgloss.Blend1D(max(w, 2), c.P.Accent, c.P.Accent2, c.P.Accent3)
	}
	return solid(c.P.Border, w)
}

func solid(c color.Color, w int) []color.Color {
	out := make([]color.Color, max(w, 2))
	for i := range out {
		out[i] = c
	}
	return out
}

func (p PanelView) Draw(c Ctx, size Size) []string {
	w, h := size.W, size.H
	if w < 4 || h < 2 {
		return block(nil, w, h)
	}
	edge := p.edge(c, w)
	pal := c.P

	tl := ""
	if p.title != "" {
		tc := pal.Sub
		if p.focused {
			tc = pal.Text
		}
		head := lipgloss.NewStyle().Foreground(tc).Bold(true)
		if p.focused {
			tl = lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).Render(" ● ") + head.Render(p.title)
		} else {
			tl = head.Render(" " + p.title)
		}
	}
	rl := ""
	if p.label != nil {
		z := Measure(c, p.label, Size{max(w-8-textx.Width(tl), 0), 1})
		if z.W > 0 {
			rl = " " + Draw(c, p.label, Size{z.W, 1})[0] + " "
		}
	}
	if p.title != "" {
		// The subtitle (often a path) gets what is left, cut in the middle
		// so both ends stay readable.
		if budget := w - 10 - textx.Width(tl) - textx.Width(rl); p.subtitle != "" && budget >= 5 {
			tl += lipgloss.NewStyle().Foreground(pal.Muted).Render("  " + textx.TruncateMiddle(p.subtitle, budget))
		}
		tl += " "
	}
	bl := ""
	if p.focused && len(p.hints) > 0 {
		bl = " " + hintLine(pal, p.hints, " · ", w-6) + " "
		if strings.TrimSpace(bl) == "" {
			bl = ""
		}
	}

	out := make([]string, 0, h)
	out = append(out, borderLine(edge, w, "╭", "╮", textx.Truncate(tl, w-4), textx.Truncate(rl, max(0, w-6-textx.Width(tl)))))
	left := lipgloss.NewStyle().Foreground(edge[0]).Render("│")
	right := lipgloss.NewStyle().Foreground(edge[w-1]).Render("│")
	for _, l := range Draw(c, p.body, Size{w - 2, h - 2}) {
		out = append(out, left+l+right)
	}
	out = append(out, borderLine(edge, w, "╰", "╯", bl, ""))
	return out
}

// borderLine draws a horizontal border with label at the left and rlabel at
// the right, both already styled.
func borderLine(edge []color.Color, w int, lc, rc, label, rlabel string) string {
	fg := func(i int) lipgloss.Style { return lipgloss.NewStyle().Foreground(edge[min(i, len(edge)-1)]) }
	var b strings.Builder
	b.WriteString(fg(0).Render(lc))
	used := 1
	if label != "" {
		b.WriteString(fg(1).Render("─"))
		b.WriteString(label)
		used += 1 + textx.Width(label)
	}
	rw := textx.Width(rlabel)
	for ; used < w-1-rw-1 && used < w-1; used++ {
		b.WriteString(fg(used).Render("─"))
	}
	if rlabel != "" && used+rw+1 < w {
		b.WriteString(rlabel)
		used += rw
		b.WriteString(fg(min(used, w-2)).Render("─"))
		used++
	}
	for ; used < w-1; used++ {
		b.WriteString(fg(used).Render("─"))
	}
	b.WriteString(fg(w - 1).Render(rc))
	return b.String()
}

// ─── Dialog ─────────────────────────────────────────────────────────────────

// DialogView is a floating card for modals: a gradient-bordered box with a
// gradient title, padded content and optional key hints at the bottom.
type DialogView struct {
	title  string
	body   []View
	hints  []Hint
	width  int
	danger bool
}

// Dialog builds a card titled title. Body views stack top to bottom.
func Dialog(title string, body ...View) DialogView {
	return DialogView{title: title, body: body, width: 60}
}

// Width sets the card's outer width (default 60).
func (d DialogView) Width(w int) DialogView { d.width = w; return d }

// Danger draws the card in the Danger colour, for destructive confirmations.
func (d DialogView) Danger(on bool) DialogView { d.danger = on; return d }

// Hints adds key hints under the content.
func (d DialogView) Hints(h ...Hint) DialogView { d.hints = h; return d }

func (d DialogView) content() View {
	var title View
	if d.danger {
		title = Text(d.title).Fg(Danger).Bold()
	} else {
		title = Gradient(d.title, Accent, Accent2, Accent3).Bold()
	}
	kids := []View{title, Height(1, Empty())}
	kids = append(kids, d.body...)
	if len(d.hints) > 0 {
		kids = append(kids, Height(1, Empty()), KeyHints(d.hints...))
	}
	return VStack(kids...)
}

func (d DialogView) Measure(c Ctx, avail Size) Size {
	w := min(d.width, avail.W)
	z := Measure(c, d.content(), Size{max(w-6, 0), max(avail.H-4, 0)})
	return Size{w, min(z.H+4, avail.H)}
}

func (d DialogView) Draw(c Ctx, size Size) []string {
	w, h := size.W, size.H
	if w < 6 || h < 4 {
		return block(nil, w, h)
	}
	var edge []color.Color
	if d.danger {
		edge = solid(c.P.Error, w)
	} else {
		edge = lipgloss.Blend1D(w, c.P.Accent, c.P.Accent2, c.P.Accent3)
	}
	inner := Draw(c, Pad(d.content(), 1, 2), Size{w - 2, h - 2})
	out := []string{borderLine(edge, w, "╭", "╮", "", "")}
	l := lipgloss.NewStyle().Foreground(edge[0]).Render("│")
	r := lipgloss.NewStyle().Foreground(edge[w-1]).Render("│")
	for _, line := range inner {
		out = append(out, l+line+r)
	}
	out = append(out, borderLine(edge, w, "╰", "╯", "", ""))
	return out
}

package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/karim-w/slop/pkg/utils/textx"
)

// Alignment places content inside a wider (or taller) box.
type Alignment int

const (
	Start Alignment = iota
	Middle
	End
)

// Style is a set of text attributes with themed colours.
type Style struct {
	fg, bg                                   Color
	bold, italic, faint, underline, reversed bool
}

// Fg sets the foreground colour.
func (s Style) Fg(c Color) Style { s.fg = c; return s }

// Bg sets the background colour.
func (s Style) Bg(c Color) Style { s.bg = c; return s }

func (s Style) Bold() Style      { s.bold = true; return s }
func (s Style) Italic() Style    { s.italic = true; return s }
func (s Style) Faint() Style     { s.faint = true; return s }
func (s Style) Underline() Style { s.underline = true; return s }
func (s Style) Reverse() Style   { s.reversed = true; return s }

// Lip resolves the style against p into a Lip Gloss style.
func (s Style) Lip(p *Palette) lipgloss.Style {
	st := lipgloss.NewStyle()
	if c := resolve(s.fg, p); c != nil {
		st = st.Foreground(c)
	}
	if c := resolve(s.bg, p); c != nil {
		st = st.Background(c)
	}
	return st.Bold(s.bold).Italic(s.italic).Faint(s.faint).Underline(s.underline).Reverse(s.reversed)
}

// Render paints s under p.
func (s Style) Render(p *Palette, text string) string { return s.Lip(p).Render(text) }

// Fg is shorthand for a style with only a foreground colour.
func Fg(c Color) Style { return Style{fg: c} }

// ─── Text ───────────────────────────────────────────────────────────────────

// TextView shows a string. It may span several lines, wrap to the space it
// gets, and carry pre-rendered ANSI from other components.
type TextView struct {
	s     string
	style Style
	align Alignment
	wrap  bool
	fill  bool
}

// Text shows s in the primary text colour.
func Text(s string) TextView { return TextView{s: s, style: Style{fg: Primary}} }

// Textf is Text with fmt.Sprintf formatting.
func Textf(format string, args ...any) TextView { return Text(fmt.Sprintf(format, args...)) }

// Raw shows a string that is already styled, such as the output of a
// Bubbles component. tui does not restyle it.
func Raw(s string) TextView { return TextView{s: s} }

func (t TextView) Fg(c Color) TextView        { t.style.fg = c; return t }
func (t TextView) Bg(c Color) TextView        { t.style.bg = c; return t }
func (t TextView) Bold() TextView             { t.style.bold = true; return t }
func (t TextView) Italic() TextView           { t.style.italic = true; return t }
func (t TextView) Faint() TextView            { t.style.faint = true; return t }
func (t TextView) Underline() TextView        { t.style.underline = true; return t }
func (t TextView) Style(s Style) TextView     { t.style = s; return t }
func (t TextView) Align(a Alignment) TextView { t.align = a; return t }

// Wrap soft-wraps the text to the width it is given instead of cutting it.
func (t TextView) Wrap() TextView { t.wrap = true; return t }

// Grow makes the text take leftover space in a stack.
func (t TextView) Grow() View { return Grow(1, t) }

func (t TextView) lines(w int) []string {
	if t.wrap && w > 0 {
		var out []string
		for _, l := range strings.Split(t.s, "\n") {
			out = append(out, textx.Wrap(l, w)...)
		}
		return out
	}
	return strings.Split(t.s, "\n")
}

// Measure: an empty Text is one blank line, handy as a vertical spacer.
func (t TextView) Measure(_ Ctx, avail Size) Size {
	lines := t.lines(avail.W)
	w := 0
	for _, l := range lines {
		w = max(w, textx.Width(l))
	}
	return Size{min(w, avail.W), min(len(lines), avail.H)}
}

func (t TextView) Draw(c Ctx, size Size) []string {
	st := t.style.Lip(c.P)
	plain := t.style == Style{}
	lines := t.lines(size.W)
	out := make([]string, 0, size.H)
	for i := 0; i < size.H; i++ {
		l := ""
		if i < len(lines) {
			l = textx.Truncate(lines[i], size.W)
		}
		gap := size.W - textx.Width(l)
		left := 0
		switch t.align {
		case Middle:
			left = gap / 2
		case End:
			left = gap
		}
		if !plain {
			l = st.Render(l)
		}
		// Padding carries the background, if any, so a tinted line is solid.
		padStyle := lipgloss.NewStyle()
		if bg := resolve(t.style.bg, c.P); bg != nil {
			padStyle = padStyle.Background(bg)
		}
		pad := func(n int) string {
			if n <= 0 {
				return ""
			}
			if t.style.bg == nil {
				return blank(n)
			}
			return padStyle.Render(blank(n))
		}
		out = append(out, pad(left)+l+pad(gap-left))
	}
	return out
}

// ─── Spans ──────────────────────────────────────────────────────────────────

// Span is a styled run of text inside a Line.
type Span struct {
	Text  string
	Style Style
}

// S builds a span: S("main", Fg(Accent).Bold()).
func S(text string, st Style) Span { return Span{text, st} }

// LineView is one line of differently styled spans, like an attributed string.
type LineView struct {
	spans []Span
	align Alignment
}

// Line joins styled spans into one line.
func Line(spans ...Span) LineView { return LineView{spans: spans} }

func (l LineView) Align(a Alignment) LineView { l.align = a; return l }

// Grow makes the line take leftover space in a stack.
func (l LineView) Grow() View { return Grow(1, l) }

func (l LineView) Measure(_ Ctx, avail Size) Size {
	w := 0
	for _, s := range l.spans {
		w += textx.Width(s.Text)
	}
	if w == 0 {
		return Size{}
	}
	return Size{min(w, avail.W), min(1, avail.H)}
}

func (l LineView) Draw(c Ctx, size Size) []string {
	return Raw(Spans(c.P, l.spans...)).Align(l.align).Draw(c, size)
}

// Spans renders spans under p into one ANSI string.
func Spans(p *Palette, spans ...Span) string {
	var b strings.Builder
	for _, s := range spans {
		if s.Style == (Style{}) {
			b.WriteString(s.Text)
			continue
		}
		b.WriteString(s.Style.Render(p, s.Text))
	}
	return b.String()
}

package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/karim-w/slop/pkg/utils/mathx"
	"github.com/karim-w/slop/pkg/utils/textx"
)

// ─── Key hints ──────────────────────────────────────────────────────────────

// Hint is one key binding shown to the user.
type Hint struct{ Key, Desc string }

// H builds a hint: H("q", "quit").
func H(key, desc string) Hint { return Hint{key, desc} }

// HintsView shows "key desc · key desc", dropping hints from the end until
// they fit.
type HintsView struct {
	hints []Hint
	sep   string
}

// KeyHints lists key bindings in the design language's style.
func KeyHints(h ...Hint) HintsView { return HintsView{hints: h, sep: "  ·  "} }

// Sep changes the separator between hints.
func (k HintsView) Sep(s string) HintsView { k.sep = s; return k }

func (k HintsView) Measure(c Ctx, avail Size) Size {
	s := hintLine(c.P, k.hints, k.sep, avail.W)
	if s == "" {
		return Size{}
	}
	return Size{textx.Width(s), 1}
}

func (k HintsView) Draw(c Ctx, size Size) []string {
	return block([]string{hintLine(c.P, k.hints, k.sep, size.W)}, size.W, size.H)
}

func hintLine(p *Palette, hints []Hint, sep string, w int) string {
	key := lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	desc := lipgloss.NewStyle().Foreground(p.Muted)
	sepS := lipgloss.NewStyle().Foreground(p.Border).Render(sep)
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = key.Render(h.Key) + " " + desc.Render(h.Desc)
	}
	for n := len(parts); n > 0; n-- {
		s := strings.Join(parts[:n], sepS)
		if textx.Width(s) <= w {
			return s
		}
	}
	return ""
}

// ─── Progress ───────────────────────────────────────────────────────────────

// BarView is a horizontal progress bar that fills the width it gets.
type BarView struct {
	frac  float64
	color Color
}

// ProgressBar shows frac (0–1) as a bar in Accent.
func ProgressBar(frac float64) BarView { return BarView{frac: frac, color: Accent} }

// Color changes the filled part's colour.
func (b BarView) Color(c Color) BarView { b.color = c; return b }

func (b BarView) flexWeight() int                { return 1 }
func (b BarView) Measure(_ Ctx, avail Size) Size { return Size{avail.W, 1} }

func (b BarView) Draw(c Ctx, size Size) []string {
	filled := int(mathx.Clamp(b.frac, 0, 1) * float64(size.W))
	bar := lipgloss.NewStyle().Foreground(resolve(b.color, c.P)).Render(strings.Repeat("━", filled)) +
		lipgloss.NewStyle().Foreground(c.P.Border).Render(strings.Repeat("━", size.W-filled))
	return block([]string{bar}, size.W, size.H)
}

// ─── Decorations ────────────────────────────────────────────────────────────

// GradientView paints text with a horizontal blend across colours.
type GradientView struct {
	text  string
	stops []Color
	bold  bool
}

// Gradient paints text blending through stops (at least one).
func Gradient(text string, stops ...Color) GradientView {
	if len(stops) == 0 {
		stops = []Color{Accent, Accent2, Accent3}
	}
	return GradientView{text: text, stops: stops}
}

func (g GradientView) Bold() GradientView { g.bold = true; return g }

func (g GradientView) Measure(_ Ctx, avail Size) Size {
	return Size{min(textx.Width(g.text), avail.W), min(1, avail.H)}
}

func (g GradientView) Draw(c Ctx, size Size) []string {
	return block([]string{GradientString(c.P, g.text, g.bold, g.stops...)}, size.W, size.H)
}

// GradientString renders text with a colour blend, for code that builds
// strings directly.
func GradientString(p *Palette, text string, bold bool, stops ...Color) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}
	colors := make([]color.Color, len(stops))
	for i, s := range stops {
		colors[i] = s.Resolve(p)
	}
	blend := lipgloss.Blend1D(max(len(runes), 2), colors...)
	var b strings.Builder
	for i, r := range runes {
		st := lipgloss.NewStyle().Foreground(blend[i]).Bold(bold)
		b.WriteString(st.Render(string(r)))
	}
	return b.String()
}

// Chip is a small badge: text on a coloured background with a cell of
// padding each side.
func Chip(text string, fg, bg Color) TextView {
	return Text(" " + text + " ").Fg(fg).Bg(bg)
}

// Divider is a horizontal rule in the Border colour that fills its width.
func Divider() View {
	return Height(1, Func(func(c Ctx, s Size) []string {
		line := lipgloss.NewStyle().Foreground(c.P.Border).Render(strings.Repeat("─", s.W))
		return block([]string{line}, s.W, s.H)
	}))
}

// VDivider is a one-cell-wide vertical rule in the Border colour that fills
// its height.
func VDivider() View {
	return Width(1, Func(func(c Ctx, s Size) []string {
		line := lipgloss.NewStyle().Foreground(c.P.Border).Render("│")
		out := make([]string, s.H)
		for i := range out {
			out[i] = line
		}
		return out
	}))
}

// Checkbox renders [✓] or [ ].
func Checkbox(on bool) TextView {
	if on {
		return Text("[✓]").Fg(Success)
	}
	return Text("[ ]").Fg(Muted)
}

package tui

import (
	"image/color"

	"github.com/karim-w/slop/pkg/domain/theme"
)

// Palette is a theme resolved into ready-to-use colours. Views never pick
// raw colours; they name a Role and the palette in the render context
// decides what it looks like. Swapping the palette re-skins the whole app.
type Palette struct {
	T                                  theme.Theme
	Bg, Surface, Overlay, Border       color.Color
	Text, Sub, Muted                   color.Color
	Accent, Accent2, Accent3           color.Color
	Success, Warning, Error, Info      color.Color
	Added, Removed, Modified, Sel      color.Color
	AddBg, DelBg, HunkBg, AddHi, DelHi color.Color
	Graph                              []color.Color
}

// NewPalette resolves t.
func NewPalette(t theme.Theme) *Palette {
	h := theme.Hex
	p := &Palette{
		T:  t,
		Bg: h(t.Bg), Surface: h(t.Surface), Overlay: h(t.Overlay), Border: h(t.Border),
		Text: h(t.Text), Sub: h(t.Subtext), Muted: h(t.Muted),
		Accent: h(t.Accent), Accent2: h(t.Accent2), Accent3: h(t.Accent3),
		Success: h(t.Success), Warning: h(t.Warning), Error: h(t.Error), Info: h(t.Info),
		Added: h(t.Added), Removed: h(t.Removed), Modified: h(t.Modified), Sel: h(t.Selection),
	}
	p.AddBg = theme.Mix(p.Bg, p.Added, 0.12)
	p.DelBg = theme.Mix(p.Bg, p.Removed, 0.12)
	p.AddHi = theme.Mix(p.Bg, p.Added, 0.22)
	p.DelHi = theme.Mix(p.Bg, p.Removed, 0.22)
	p.HunkBg = theme.Mix(p.Bg, p.Accent, 0.10)
	for _, g := range t.Graph {
		p.Graph = append(p.Graph, h(g))
	}
	if len(p.Graph) == 0 {
		p.Graph = []color.Color{p.Accent}
	}
	return p
}

// DefaultPalette resolves the default theme.
func DefaultPalette() *Palette { return NewPalette(theme.Default()) }

// Dimmed returns the palette faded toward the background, for the scrim
// behind a modal.
func (p *Palette) Dimmed(amount float64) *Palette { return NewPalette(p.T.Dimmed(amount)) }

// Lane returns the i-th categorical colour, wrapping around.
func (p *Palette) Lane(i int) color.Color {
	if i < 0 {
		i = -i
	}
	return p.Graph[i%len(p.Graph)]
}

// Color is anything a view can be painted with: a semantic Role, resolved
// against the current palette, or a fixed colour from RGB or Hex.
type Color interface {
	Resolve(p *Palette) color.Color
}

// Role is a semantic colour slot in the design language.
type Role int

// The roles of the design language. Prefer these over fixed colours so
// views follow the theme.
const (
	Background Role = iota // app background
	Surface                // raised areas: chips, panels
	Overlay                // dialogs
	Border                 // inactive borders, separators
	Primary                // body text
	Secondary              // supporting text
	Muted                  // hints, timestamps, disabled
	Accent                 // brand colour, focus
	Accent2                // gradient partner
	Accent3                // gradient partner
	Success
	Warning
	Danger
	Info
	Selection // background of the row under the cursor
	Added
	Removed
	Modified
)

// Resolve implements Color.
func (r Role) Resolve(p *Palette) color.Color {
	switch r {
	case Background:
		return p.Bg
	case Surface:
		return p.Surface
	case Overlay:
		return p.Overlay
	case Border:
		return p.Border
	case Primary:
		return p.Text
	case Secondary:
		return p.Sub
	case Muted:
		return p.Muted
	case Accent:
		return p.Accent
	case Accent2:
		return p.Accent2
	case Accent3:
		return p.Accent3
	case Success:
		return p.Success
	case Warning:
		return p.Warning
	case Danger:
		return p.Error
	case Info:
		return p.Info
	case Selection:
		return p.Sel
	case Added:
		return p.Added
	case Removed:
		return p.Removed
	case Modified:
		return p.Modified
	}
	return p.Text
}

// Lane is the i-th categorical colour of the palette (graph lanes, avatars,
// tags). Use it to tell many things apart while staying on-theme.
type Lane int

// Resolve implements Color.
func (l Lane) Resolve(p *Palette) color.Color { return p.Lane(int(l)) }

type fixed struct{ c color.Color }

func (f fixed) Resolve(*Palette) color.Color { return f.c }

// RGB wraps a fixed colour. It ignores the theme; use it sparingly.
func RGB(c color.Color) Color { return fixed{c} }

// Hex is a fixed colour from "#rrggbb".
func Hex(s string) Color { return fixed{theme.Hex(s)} }

type mixed struct {
	a, b Color
	t    float64
}

func (m mixed) Resolve(p *Palette) color.Color {
	return theme.Mix(m.a.Resolve(p), m.b.Resolve(p), m.t)
}

// Mix blends a toward b by t (0 = a, 1 = b) under the current palette.
// Mix(Background, Accent, 0.15) is a faint accent tint.
func Mix(a, b Color, t float64) Color { return mixed{a, b, t} }

func resolve(c Color, p *Palette) color.Color {
	if c == nil {
		return nil
	}
	return c.Resolve(p)
}

package theme

import (
	"image/color"
	"math"
)

// TermColors holds the colours a host terminal reports: its default
// foreground and background and its 16-colour ANSI palette.
type TermColors struct {
	Fg, Bg color.Color
	ANSI   [16]color.Color
}

// Complete reports whether enough colours arrived to build a theme.
func (tc TermColors) Complete() bool { return tc.Bg != nil && tc.Fg != nil }

// Luminance is the WCAG relative luminance of c, from 0 (black) to 1 (white).
func Luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	f := func(v uint32) float64 {
		x := float64(v>>8) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*f(r) + 0.7152*f(g) + 0.0722*f(b)
}

// Contrast is the WCAG contrast ratio between a and b, from 1 to 21.
func Contrast(a, b color.Color) float64 {
	la, lb := Luminance(a), Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// FromTerminal derives a full theme named name from the terminal's colours,
// so an app can match whatever theme the host terminal is using.
func FromTerminal(name string, tc TermColors) Theme {
	base := Builtins[0]
	bg, fg := tc.Bg, tc.Fg
	if bg == nil {
		bg = Hex(base.Bg)
	}
	if fg == nil {
		fg = Hex(base.Text)
	}
	dark := Luminance(bg) < 0.4
	// pick an ANSI colour, preferring the bright variant when it reads better
	pick := func(normal int, fallback string) color.Color {
		n, b := tc.ANSI[normal], tc.ANSI[normal+8]
		switch {
		case n == nil && b == nil:
			return Hex(fallback)
		case n == nil:
			return b
		case b == nil:
			return n
		}
		if Contrast(b, bg) > Contrast(n, bg) {
			return b
		}
		return n
	}
	red, green, yellow := pick(1, base.Error), pick(2, base.Success), pick(3, base.Warning)
	blue, magenta, cyan := pick(4, base.Info), pick(5, base.Accent), pick(6, base.Accent3)

	muted := Mix(fg, bg, 0.52)
	if c := tc.ANSI[8]; c != nil && Contrast(c, bg) > 2.2 {
		muted = c
	}
	h := ToHex
	t := Theme{
		Name:      name,
		Dark:      dark,
		Bg:        h(bg),
		Surface:   h(Mix(bg, fg, 0.07)),
		Overlay:   h(Mix(bg, fg, 0.04)),
		Border:    h(Mix(bg, fg, 0.22)),
		Text:      h(fg),
		Subtext:   h(Mix(fg, bg, 0.22)),
		Muted:     h(muted),
		Accent:    h(magenta),
		Accent2:   h(blue),
		Accent3:   h(cyan),
		Success:   h(green),
		Warning:   h(yellow),
		Error:     h(red),
		Info:      h(blue),
		Added:     h(green),
		Removed:   h(red),
		Modified:  h(yellow),
		Selection: h(Mix(bg, magenta, 0.16)),
	}
	t.Graph = []string{h(magenta), h(cyan), h(yellow), h(green), h(blue), h(red)}
	for _, i := range []int{13, 14, 11, 10, 12, 9} {
		if c := tc.ANSI[i]; c != nil {
			t.Graph = append(t.Graph, h(c))
		}
	}
	return t
}

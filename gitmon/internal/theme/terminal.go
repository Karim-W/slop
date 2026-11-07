package theme

import (
	"image/color"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// TermColors holds colours reported by the host terminal through OSC 10, 11
// and 4 queries.
type TermColors struct {
	Fg, Bg color.Color
	ANSI   [16]color.Color
}

// InRex reports whether gitmon runs inside a Rex terminal.
func InRex() bool { return os.Getenv("REX_SESSION") != "" || os.Getenv("REX_SERVER") != "" }

// TerminalName is the name of the theme that mirrors the host terminal.
func TerminalName() string {
	if InRex() {
		return "rex"
	}
	return "terminal"
}

// QuerySequence asks the terminal for its default colours and its 16 ANSI
// palette entries.
func QuerySequence() string {
	var b strings.Builder
	b.WriteString("\x1b]10;?\x1b\\\x1b]11;?\x1b\\")
	for i := 0; i < 16; i++ {
		b.WriteString("\x1b]4;" + strconv.Itoa(i) + ";?\x1b\\")
	}
	return b.String()
}

// ParseOSC4 parses a reply such as "\x1b]4;1;rgb:ffff/0000/0000\x1b\\".
func ParseOSC4(s string) (int, color.Color, bool) {
	s = strings.TrimPrefix(s, "\x1b]")
	s = strings.TrimPrefix(s, "\x9d")
	s = strings.TrimSuffix(s, "\x1b\\")
	s = strings.TrimSuffix(s, "\x07")
	s = strings.TrimSuffix(s, "\x9c")
	if !strings.HasPrefix(s, "4;") {
		return 0, nil, false
	}
	parts := strings.SplitN(s[2:], ";", 2)
	if len(parts) != 2 {
		return 0, nil, false
	}
	idx, err := strconv.Atoi(parts[0])
	if err != nil || idx < 0 || idx > 15 {
		return 0, nil, false
	}
	c := ansi.XParseColor(parts[1])
	if c == nil {
		return 0, nil, false
	}
	return idx, c, true
}

// Complete reports whether enough colours arrived to build a theme.
func (tc TermColors) Complete() bool { return tc.Bg != nil && tc.Fg != nil }

func lum(c color.Color) float64 {
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

func contrast(a, b color.Color) float64 {
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// FromTerminal derives a full gitmon theme from the terminal's colours, so
// gitmon always matches whatever theme Rex (or any terminal) is using.
func FromTerminal(tc TermColors) Theme {
	base := Builtins[0]
	bg, fg := tc.Bg, tc.Fg
	if bg == nil {
		bg = Hex(base.Bg)
	}
	if fg == nil {
		fg = Hex(base.Text)
	}
	dark := lum(bg) < 0.4
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
		if contrast(b, bg) > contrast(n, bg) {
			return b
		}
		return n
	}
	red, green, yellow := pick(1, base.Error), pick(2, base.Success), pick(3, base.Warning)
	blue, magenta, cyan := pick(4, base.Info), pick(5, base.Accent), pick(6, base.Accent3)

	muted := Mix(fg, bg, 0.52)
	if c := tc.ANSI[8]; c != nil && contrast(c, bg) > 2.2 {
		muted = c
	}
	h := ToHex
	t := Theme{
		Name:      TerminalName(),
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

// Package textx measures and shapes terminal text. Every function is aware
// of ANSI escape sequences and wide runes, so styled strings keep their
// colours and still line up in cells.
package textx

import (
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Ellipsis is appended to text cut short by Truncate.
const Ellipsis = "…"

// Width is the number of terminal cells s occupies.
func Width(s string) int { return ansi.StringWidth(s) }

// Truncate cuts s to at most w cells, ending with an ellipsis when cut.
func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, Ellipsis)
}

// Clip cuts s to at most w cells without an ellipsis.
func Clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "")
}

// Pad right-pads (or truncates) s to exactly w cells.
func Pad(s string, w int) string {
	s = Truncate(s, w)
	if d := w - ansi.StringWidth(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// PadLeft left-pads (or truncates) s to exactly w cells.
func PadLeft(s string, w int) string {
	s = Truncate(s, w)
	if d := w - ansi.StringWidth(s); d > 0 {
		s = strings.Repeat(" ", d) + s
	}
	return s
}

// Center pads s on both sides to exactly w cells.
func Center(s string, w int) string {
	s = Truncate(s, w)
	d := w - ansi.StringWidth(s)
	if d <= 0 {
		return s
	}
	return strings.Repeat(" ", d/2) + s + strings.Repeat(" ", d-d/2)
}

// TruncateMiddle keeps both ends of plain text visible: /home/…/project/src.
func TruncateMiddle(s string, w int) string {
	runes := []rune(s)
	if len(runes) <= w || w < 5 {
		return s
	}
	keep := w - 1
	head := keep / 3
	tail := keep - head
	return string(runes[:head]) + Ellipsis + string(runes[len(runes)-tail:])
}

// Wrap soft-wraps s at word boundaries to lines of at most w cells.
func Wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	out := strings.Split(ansi.Wrap(s, w, ""), "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	return out
}

// FirstLine returns s up to its first newline.
func FirstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// Lines splits s on newlines; an empty string yields no lines.
func Lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// Pluralize renders "1 file" or "3 files".
func Pluralize(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + plural
}

// Initials turns "Ada Lovelace" into "AL" and "ada" into "AD".
func Initials(name string) string {
	f := strings.Fields(name)
	switch {
	case len(f) == 0:
		return "??"
	case len(f) == 1:
		r := []rune(f[0])
		if len(r) >= 2 {
			return strings.ToUpper(string(r[:2]))
		}
		return strings.ToUpper(string(r))
	}
	a, b := []rune(f[0]), []rune(f[len(f)-1])
	return strings.ToUpper(string(a[0]) + string(b[0]))
}

// Hash maps s to a stable non-negative int, handy for picking a colour.
func Hash(s string) int {
	h := fnv.New32a()
	h.Write([]byte(s))
	return int(h.Sum32() & 0x7fffffff)
}

// CutLeft drops the first n cells of s, keeping its styling.
func CutLeft(s string, n int) string {
	if n <= 0 {
		return s
	}
	return ansi.TruncateLeft(s, n, "")
}

package tui

import (
	"fmt"
	"image/color"
	"strings"
)

// ─── Padding ────────────────────────────────────────────────────────────────

type padded struct {
	child                    View
	top, right, bottom, left int
}

// Pad surrounds v with blank space: Pad(v, 1) on all sides, Pad(v, 1, 2)
// vertical then horizontal, Pad(v, t, r, b, l) like CSS.
func Pad(v View, n ...int) View {
	p := padded{child: v}
	switch len(n) {
	case 1:
		p.top, p.right, p.bottom, p.left = n[0], n[0], n[0], n[0]
	case 2:
		p.top, p.bottom, p.left, p.right = n[0], n[0], n[1], n[1]
	case 4:
		p.top, p.right, p.bottom, p.left = n[0], n[1], n[2], n[3]
	}
	return p
}

func (p padded) flexWeight() int { return weightOf(p.child) }

func (p padded) inner(s Size) Size {
	return Size{max(s.W-p.left-p.right, 0), max(s.H-p.top-p.bottom, 0)}
}

func (p padded) Measure(c Ctx, avail Size) Size {
	z := Measure(c, p.child, p.inner(avail))
	return Size{z.W + p.left + p.right, z.H + p.top + p.bottom}
}

func (p padded) Draw(c Ctx, size Size) []string {
	in := p.inner(size)
	out := block(nil, size.W, p.top)
	for _, l := range Draw(c, p.child, in) {
		out = append(out, blank(p.left)+l+blank(size.W-p.left-in.W))
	}
	return block(out, size.W, size.H)
}

// ─── Fixed sizes ────────────────────────────────────────────────────────────

type sized struct {
	child View
	w, h  int // -1 = child's own
}

// Width fixes v's width to w cells (or less, if less is available).
func Width(w int, v View) View { return sized{v, w, -1} }

// Height fixes v's height to h lines.
func Height(h int, v View) View { return sized{v, -1, h} }

// Frame fixes both dimensions.
func Frame(w, h int, v View) View { return sized{v, w, h} }

func (s sized) Measure(c Ctx, avail Size) Size {
	in := avail
	if s.w >= 0 {
		in.W = min(s.w, avail.W)
	}
	if s.h >= 0 {
		in.H = min(s.h, avail.H)
	}
	z := Measure(c, s.child, in)
	if s.w >= 0 {
		z.W = in.W
	}
	if s.h >= 0 {
		z.H = in.H
	}
	return z
}

func (s sized) Draw(c Ctx, size Size) []string { return Draw(c, s.child, size) }

// ─── Alignment ──────────────────────────────────────────────────────────────

type aligned struct {
	child View
	h, v  Alignment
}

// Align fills the space it gets and places v inside it.
func Align(h, v Alignment, child View) View { return aligned{child, h, v} }

// Center fills the space it gets and centres v in it.
func Center(v View) View { return aligned{v, Middle, Middle} }

func (a aligned) Measure(_ Ctx, avail Size) Size { return avail }
func (a aligned) flexWeight() int                { return 1 }

func (a aligned) Draw(c Ctx, size Size) []string {
	z := Measure(c, a.child, size)
	x := offset(a.h, size.W-z.W)
	y := offset(a.v, size.H-z.H)
	out := block(nil, size.W, y)
	for _, l := range Draw(c, a.child, z) {
		out = append(out, blank(x)+l+blank(size.W-x-z.W))
	}
	return block(out, size.W, size.H)
}

func offset(a Alignment, gap int) int {
	switch a {
	case Middle:
		return max(gap/2, 0)
	case End:
		return max(gap, 0)
	}
	return 0
}

// ─── Background ─────────────────────────────────────────────────────────────

type tinted struct {
	child View
	bg    Color
}

// Tint draws v on a solid background. Styled text inside keeps the
// background even where it resets its own colours.
func Tint(bg Color, v View) View { return tinted{v, bg} }

func (t tinted) Measure(c Ctx, avail Size) Size { return Measure(c, t.child, avail) }
func (t tinted) flexWeight() int                { return weightOf(t.child) }

func (t tinted) Draw(c Ctx, size Size) []string {
	lines := Draw(c, t.child, size)
	bg := resolve(t.bg, c.P)
	for i, l := range lines {
		lines[i] = Highlight(l, bg)
	}
	return lines
}

// Highlight paints line on bg, re-applying the background after every
// reset inside the line so nested styles don't punch holes in it.
func Highlight(line string, bg color.Color) string {
	if bg == nil {
		return line
	}
	r, g, b, _ := bg.RGBA()
	on := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+on)
	line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+on)
	line = strings.ReplaceAll(line, "\x1b[49m", on)
	return on + line + "\x1b[0m"
}

package tui

import (
	"github.com/karim-w/slop/pkg/utils/textx"
)

// Layer is a view floated over a ZStack's base.
type Layer struct {
	View View
	// Where to anchor the layer inside the base, per axis.
	H, V Alignment
	// DX and DY nudge the layer from its anchor, such as a spring offset
	// for a slide-in animation.
	DX, DY int
}

// Float anchors v at the centre.
func Float(v View) Layer { return Layer{View: v, H: Middle, V: Middle} }

// At anchors v at the given alignment.
func At(h, vAlign Alignment, v View) Layer { return Layer{View: v, H: h, V: vAlign} }

// Offset nudges the layer.
func (l Layer) Offset(dx, dy int) Layer { l.DX, l.DY = dx, dy; return l }

// ZStackView draws layers on top of a base, like SwiftUI's ZStack. Each
// layer is drawn at its measured size; the base shows around it.
type ZStackView struct {
	base   View
	layers []Layer
	scrim  float64
}

// ZStack floats layers over base. Layers with nil views are skipped.
func ZStack(base View, layers ...Layer) ZStackView { return ZStackView{base: base, layers: layers} }

// Modal floats dialog in the centre and fades base behind it. A nil dialog
// draws base unchanged.
func Modal(base View, dialog View) ZStackView {
	if dialog == nil {
		return ZStack(base)
	}
	return ZStackView{base: base, layers: []Layer{Float(dialog)}, scrim: 0.55}
}

// Scrim fades the base toward the background by amount (0–1) while any
// layer is shown. Views that use Roles fade properly; raw ANSI does not.
func (z ZStackView) Scrim(amount float64) ZStackView { z.scrim = amount; return z }

func (z ZStackView) flexWeight() int { return max(weightOf(z.base), 1) }

func (z ZStackView) Measure(c Ctx, avail Size) Size { return avail }

func (z ZStackView) Draw(c Ctx, size Size) []string {
	live := 0
	for _, l := range z.layers {
		if l.View != nil {
			live++
		}
	}
	bc := c
	if z.scrim > 0 && live > 0 {
		bc.P = c.P.Dimmed(z.scrim)
	}
	out := Draw(bc, z.base, size)
	for _, l := range z.layers {
		if l.View == nil {
			continue
		}
		ls := Measure(c, l.View, size)
		x := offset(l.H, size.W-ls.W) + l.DX
		y := offset(l.V, size.H-ls.H) + l.DY
		paste(out, Draw(c, l.View, ls), x, y, size.W)
	}
	return out
}

// paste writes fg into bg at (x, y), clipping at the edges.
func paste(bg, fg []string, x, y, w int) {
	for i, line := range fg {
		row := y + i
		if row < 0 || row >= len(bg) {
			continue
		}
		lw := textx.Width(line)
		if x < 0 {
			line = textx.CutLeft(line, -x)
			lw += x
			x = 0
		}
		if x+lw > w {
			line = textx.Clip(line, w-x)
			lw = w - x
		}
		if lw <= 0 {
			continue
		}
		left := textx.Clip(bg[row], x)
		right := textx.CutLeft(bg[row], x+lw)
		bg[row] = left + "\x1b[0m" + line + "\x1b[0m" + right
	}
}

// Package anim animates scalar values with damped springs, for smooth
// scrolling, sliding panels and popping dialogs in Bubble Tea apps.
//
// A Spring holds a position and a target. Each frame, call Step on every
// live spring and keep requesting frames (with Frame) while any of them is
// still moving.
package anim

import (
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/harmonica"

	"github.com/karim-w/slop/pkg/utils/mathx"
)

// FPS is the animation frame rate.
const FPS = 60

// Presets shared by every app, so motion feels the same everywhere.
var (
	// Scroll is critically damped: fast, no overshoot. Lists and viewports.
	Scroll = harmonica.NewSpring(harmonica.FPS(FPS), 18.0, 1.0)
	// Panel is slightly bouncy. Panels resizing or sliding.
	Panel = harmonica.NewSpring(harmonica.FPS(FPS), 14.0, 0.82)
	// Pop overshoots a little. Dialogs and toasts appearing.
	Pop = harmonica.NewSpring(harmonica.FPS(FPS), 16.0, 0.68)
)

// FrameMsg is delivered once per animation frame.
type FrameMsg struct{}

// Frame schedules the next FrameMsg.
func Frame() tea.Cmd {
	return tea.Tick(time.Second/FPS, func(time.Time) tea.Msg { return FrameMsg{} })
}

// Spring is an animated scalar.
type Spring struct {
	Pos, Vel, Target float64
	s                *harmonica.Spring
}

// NewSpring returns a spring at rest at v, moving with preset s.
func NewSpring(s *harmonica.Spring, v float64) Spring {
	return Spring{Pos: v, Target: v, s: s}
}

// Set moves the target; the position follows over the next frames.
func (a *Spring) Set(t float64) { a.Target = t }

// Snap jumps to t at once.
func (a *Spring) Snap(t float64) { a.Pos, a.Vel, a.Target = t, 0, t }

// Step advances one frame and reports whether the spring is still moving.
func (a *Spring) Step() bool {
	if a.Settled() {
		a.Pos, a.Vel = a.Target, 0
		return false
	}
	if a.s == nil {
		a.s = &Scroll
	}
	a.Pos, a.Vel = a.s.Update(a.Pos, a.Vel, a.Target)
	return !a.Settled()
}

// Settled reports whether the spring has come to rest at its target.
func (a *Spring) Settled() bool {
	return math.Abs(a.Pos-a.Target) < 0.02 && math.Abs(a.Vel) < 0.05
}

// Int is the position rounded to the nearest cell.
func (a *Spring) Int() int { return int(math.Round(a.Pos)) }

// Scroller is a spring-driven scroll offset that keeps a cursor in view.
type Scroller struct{ Spring }

// NewScroller returns a scroller at the top.
func NewScroller() Scroller { return Scroller{Spring: NewSpring(&Scroll, 0)} }

// Follow retargets the offset so line sel stays visible, with a small
// margin, in a window of height rows over total rows.
func (s *Scroller) Follow(sel, height, total int) {
	if height <= 0 {
		return
	}
	margin := min(3, height/4)
	t := int(math.Round(s.Target))
	if sel < t+margin {
		t = sel - margin
	}
	if sel > t+height-1-margin {
		t = sel - height + 1 + margin
	}
	s.Target = float64(mathx.Clamp(t, 0, max(0, total-height)))
}

// Offset is the first visible row for a window of height over total rows.
func (s *Scroller) Offset(height, total int) int {
	return mathx.Clamp(s.Int(), 0, max(0, total-height))
}

// Window implements tui.Scroller, so a list can scroll with this spring.
// Call Follow from your key handlers; Window only reports the offset.
func (s *Scroller) Window(_, rows, total int) int { return s.Offset(rows, total) }

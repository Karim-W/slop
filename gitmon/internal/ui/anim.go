package ui

import (
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/harmonica"
)

const fps = 60

var (
	scrollSpring = harmonica.NewSpring(harmonica.FPS(fps), 18.0, 1.0)
	panelSpring  = harmonica.NewSpring(harmonica.FPS(fps), 14.0, 0.82)
	popSpring    = harmonica.NewSpring(harmonica.FPS(fps), 16.0, 0.68)
)

type frameMsg struct{}

func frame() tea.Cmd {
	return tea.Tick(time.Second/fps, func(time.Time) tea.Msg { return frameMsg{} })
}

// spring is an animated scalar.
type spring struct {
	pos, vel, target float64
	s                *harmonica.Spring
}

func newSpring(s *harmonica.Spring, v float64) spring {
	return spring{pos: v, target: v, s: s}
}

func (a *spring) set(t float64) { a.target = t }

func (a *spring) snap(t float64) { a.pos, a.vel, a.target = t, 0, t }

// step advances one frame and reports whether it is still moving.
func (a *spring) step() bool {
	if a.settled() {
		a.pos, a.vel = a.target, 0
		return false
	}
	a.pos, a.vel = a.s.Update(a.pos, a.vel, a.target)
	return !a.settled()
}

func (a *spring) settled() bool {
	return math.Abs(a.pos-a.target) < 0.02 && math.Abs(a.vel) < 0.05
}

func (a *spring) int() int { return int(math.Round(a.pos)) }

// scroller keeps a cursor visible within a window, with smooth motion.
type scroller struct {
	spring
	sel int
}

func newScroller() scroller { return scroller{spring: newSpring(&scrollSpring, 0)} }

// follow retargets the scroll offset so line stays visible with a margin.
func (s *scroller) follow(sel, height, total int) {
	if height <= 0 {
		return
	}
	margin := min(3, height/4)
	t := int(math.Round(s.target))
	if sel < t+margin {
		t = sel - margin
	}
	if sel > t+height-1-margin {
		t = sel - height + 1 + margin
	}
	t = clamp(t, 0, max(0, total-height))
	s.target = float64(t)
}

func (s *scroller) offset(height, total int) int {
	return clamp(s.int(), 0, max(0, total-height))
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

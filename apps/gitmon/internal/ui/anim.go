package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/pkg/tui/anim"
	"github.com/karim-w/slop/pkg/utils/mathx"
)

// Motion comes from the shared anim package so every app moves alike.

var (
	scrollSpring = anim.Scroll
	popSpring    = anim.Pop
)

type frameMsg = anim.FrameMsg

func frame() tea.Cmd { return anim.Frame() }

type spring = anim.Spring

var newSpring = anim.NewSpring

// scroller keeps a cursor visible within a window, with smooth motion.
type scroller struct {
	anim.Scroller
	sel int
}

func newScroller() scroller { return scroller{Scroller: anim.NewScroller()} }

func clamp(v, lo, hi int) int { return mathx.Clamp(v, lo, hi) }

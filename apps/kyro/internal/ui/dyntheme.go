package ui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/kyro/internal/art"
	"github.com/karim-w/slop/apps/kyro/internal/dyntheme"
	"github.com/karim-w/slop/apps/kyro/internal/home"
	"github.com/karim-w/slop/pkg/domain/theme"
	"github.com/karim-w/slop/pkg/tui"
)

// The dynamic theme: with dynamic_theme on, the whole palette comes from
// the cover of the playing song and fades to the next cover's on a track
// change. Without a song, or for a song without art, the base theme shows.

// fadeSteps × fadeFrame is how long a change of palette takes.
const (
	fadeSteps = 14
	fadeFrame = 30 * time.Millisecond
)

type fade struct {
	from, to theme.Theme
	cover    bool // to is a cover theme
	step     int
	gen      int // bumps on every new fade, retiring the old one's ticks
}

type fadeMsg struct{ gen int }

// coverThemeMsg delivers a theme derived off the UI goroutine.
type coverThemeMsg struct {
	key   string
	theme *theme.Theme // nil: the cover has no art
}

func baseTarget(p *tui.Palette) string { return fmt.Sprintf("base:%p", p) }

// retheme heads the palette for what should show now. It runs after every
// message and does nothing when the target hasn't changed. instant skips
// the fade (for previews).
func (m *Model) retheme(instant bool) tea.Cmd {
	target, th, cover := baseTarget(m.base), m.base.T, false
	if s := m.pl.State(); m.cfg.DynamicTheme && s.Active {
		key := art.Key(s.Track)
		ct, known := m.covers[key]
		switch {
		case !known:
			if m.coverBusy == key {
				return nil // keep the current palette until it's ready
			}
			m.coverBusy = key
			c, t, base := m.art, s.Track, m.base.T
			return func() tea.Msg {
				img := c.Image(t)
				if img == nil {
					return coverThemeMsg{key: key}
				}
				th := dyntheme.From(img, base)
				return coverThemeMsg{key: key, theme: &th}
			}
		case ct != nil:
			target, th, cover = "cover:"+key, *ct, true
		}
	}
	if target == m.themeTarget {
		if instant && m.fade.step < fadeSteps && m.fade.gen > 0 {
			m.finishFade()
		}
		return nil
	}
	m.themeTarget = target
	m.fade = fade{from: m.pal.T, to: th, cover: cover, gen: m.fade.gen + 1}
	if cover {
		m.coverOn = true
	}
	if instant || m.w == 0 {
		m.finishFade()
		return nil
	}
	return fadeTick(m.fade.gen)
}

func fadeTick(gen int) tea.Cmd {
	return tea.Tick(fadeFrame, func(time.Time) tea.Msg { return fadeMsg{gen} })
}

func (m *Model) onFade(msg fadeMsg) tea.Cmd {
	if msg.gen != m.fade.gen || m.fade.step >= fadeSteps {
		return nil
	}
	m.fade.step++
	if m.fade.step >= fadeSteps {
		m.finishFade()
		return nil
	}
	x := float64(m.fade.step) / fadeSteps
	ease := x * x * (3 - 2*x) // smoothstep
	m.setPalette(tui.NewPalette(dyntheme.Blend(m.fade.from, m.fade.to, ease)))
	return fadeTick(m.fade.gen)
}

func (m *Model) finishFade() {
	m.fade.step = fadeSteps
	if m.fade.cover {
		m.setPalette(tui.NewPalette(m.fade.to))
	} else {
		m.setPalette(m.base) // the base palette itself, so later base changes show
	}
	m.coverOn = m.fade.cover
}

// toggleDynamic turns the cover theme on or off and saves the choice.
func (m *Model) toggleDynamic() tea.Cmd {
	m.cfg.DynamicTheme = !m.cfg.DynamicTheme
	if err := home.Save(m.cfg); err != nil {
		return m.fail(err)
	}
	if !m.cfg.DynamicTheme {
		return m.showFlash("Dynamic theme off · "+m.base.T.Name, true)
	}
	if !m.pl.State().Active {
		return m.showFlash("Dynamic theme on · colours follow the cover once a song plays", true)
	}
	return m.showFlash("Dynamic theme on · colours follow the cover", true)
}

package ui

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/karim-w/slop/pkg/domain/theme"
	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/utils/humanize"
	"github.com/karim-w/slop/pkg/utils/textx"
)

// Styles is the resolved palette for one theme: the shared design-language
// palette from pkg/tui.
type Styles = tui.Palette

func newStyles(t theme.Theme) *Styles { return tui.NewPalette(t) }

func fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// on returns a style with optional background (nil = none).
func on(f, b color.Color) lipgloss.Style {
	st := lipgloss.NewStyle().Foreground(f)
	if b != nil {
		st = st.Background(b)
	}
	return st
}

// chip renders a small rounded-feeling badge.
func chip(text string, f, b color.Color, bold bool) string {
	st := lipgloss.NewStyle().Foreground(f).Background(b).Padding(0, 1)
	if bold {
		st = st.Bold(true)
	}
	return st.Render(text)
}

var (
	truncate    = textx.Truncate
	pad         = textx.Pad
	relTime     = humanize.Ago
	relTimeLong = humanize.AgoLong
	initials    = textx.Initials
	hashIndex   = textx.Hash
)

func padBg(s string, w int, bg color.Color) string {
	s = truncate(s, w)
	if d := w - ansi.StringWidth(s); d > 0 {
		if bg != nil {
			s += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", d))
		} else {
			s += strings.Repeat(" ", d)
		}
	}
	return s
}

func avatar(s *Styles, name string, bg color.Color) string {
	c := s.Lane(hashIndex(name))
	return lipgloss.NewStyle().Foreground(s.Bg).Background(c).Bold(true).Render(initials(name)) + on(s.Text, bg).Render("")
}

// huhTheme maps our palette onto huh forms.
func huhTheme(s *Styles) huh.Theme {
	return huh.ThemeFunc(func(bool) *huh.Styles {
		t := huh.ThemeBase(true)
		f := &t.Focused
		f.Base = f.Base.BorderForeground(s.Accent)
		f.Card = f.Base
		f.Title = fg(s.Accent).Bold(true)
		f.NoteTitle = fg(s.Accent).Bold(true).MarginBottom(1)
		f.Description = fg(s.Muted)
		f.ErrorIndicator = fg(s.Error).SetString(" ✗")
		f.ErrorMessage = fg(s.Error).SetString(" ✗")
		f.SelectSelector = fg(s.Accent2).SetString("› ")
		f.NextIndicator = fg(s.Accent2).MarginLeft(1).SetString("→")
		f.PrevIndicator = fg(s.Accent2).MarginRight(1).SetString("←")
		f.Option = fg(s.Text)
		f.MultiSelectSelector = fg(s.Accent2).SetString("› ")
		f.SelectedOption = fg(s.Success)
		f.SelectedPrefix = fg(s.Success).SetString("✓ ")
		f.UnselectedOption = fg(s.Text)
		f.UnselectedPrefix = fg(s.Muted).SetString("· ")
		f.FocusedButton = f.FocusedButton.Foreground(s.Bg).Background(s.Accent).Bold(true)
		f.BlurredButton = f.BlurredButton.Foreground(s.Sub).Background(s.Surface)
		f.TextInput.Cursor = fg(s.Accent2)
		f.TextInput.CursorText = fg(s.Bg).Background(s.Accent2)
		f.TextInput.Placeholder = fg(s.Muted)
		f.TextInput.Prompt = fg(s.Accent2)
		f.TextInput.Text = fg(s.Text)

		t.Blurred = t.Focused
		t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Card = t.Blurred.Base
		t.Blurred.Title = fg(s.Sub)
		t.Blurred.MultiSelectSelector = lipgloss.NewStyle().SetString("  ")
		t.Blurred.NextIndicator = lipgloss.NewStyle()
		t.Blurred.PrevIndicator = lipgloss.NewStyle()
		t.Blurred.TextInput.Prompt = fg(s.Muted)

		t.Group.Title = f.Title
		t.Group.Description = f.Description
		t.Help = help.Styles{
			Ellipsis:       fg(s.Muted),
			ShortKey:       fg(s.Sub),
			ShortDesc:      fg(s.Muted),
			ShortSeparator: fg(s.Border),
			FullKey:        fg(s.Sub),
			FullDesc:       fg(s.Muted),
			FullSeparator:  fg(s.Border),
		}
		return t
	})
}

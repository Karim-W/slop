package ui

import (
	"fmt"
	"hash/fnv"
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/karim-w/gitmon/internal/theme"
)

// Styles is the resolved palette for one theme.
type Styles struct {
	T                                  theme.Theme
	Bg, Surface, Overlay, Border       color.Color
	Text, Sub, Muted                   color.Color
	Accent, Accent2, Accent3           color.Color
	Success, Warning, Error, Info      color.Color
	Added, Removed, Modified, Sel      color.Color
	AddBg, DelBg, HunkBg, AddHi, DelHi color.Color
	Graph                              []color.Color
}

func newStyles(t theme.Theme) *Styles {
	h := theme.Hex
	s := &Styles{
		T:  t,
		Bg: h(t.Bg), Surface: h(t.Surface), Overlay: h(t.Overlay), Border: h(t.Border),
		Text: h(t.Text), Sub: h(t.Subtext), Muted: h(t.Muted),
		Accent: h(t.Accent), Accent2: h(t.Accent2), Accent3: h(t.Accent3),
		Success: h(t.Success), Warning: h(t.Warning), Error: h(t.Error), Info: h(t.Info),
		Added: h(t.Added), Removed: h(t.Removed), Modified: h(t.Modified), Sel: h(t.Selection),
	}
	s.AddBg = theme.Mix(s.Bg, s.Added, 0.12)
	s.DelBg = theme.Mix(s.Bg, s.Removed, 0.12)
	s.AddHi = theme.Mix(s.Bg, s.Added, 0.22)
	s.DelHi = theme.Mix(s.Bg, s.Removed, 0.22)
	s.HunkBg = theme.Mix(s.Bg, s.Accent, 0.10)
	for _, g := range t.Graph {
		s.Graph = append(s.Graph, h(g))
	}
	if len(s.Graph) == 0 {
		s.Graph = []color.Color{s.Accent}
	}
	return s
}

func (s *Styles) lane(i int) color.Color { return s.Graph[i%len(s.Graph)] }

func fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// on returns a style with optional background (nil = none).
func on(f, b color.Color) lipgloss.Style {
	st := lipgloss.NewStyle().Foreground(f)
	if b != nil {
		st = st.Background(b)
	}
	return st
}

// gradient paints text with a horizontal colour blend.
func gradient(text string, bold bool, stops ...color.Color) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}
	cols := lipgloss.Blend1D(max(len(runes), 2), stops...)
	var b strings.Builder
	for i, r := range runes {
		st := fg(cols[i])
		if bold {
			st = st.Bold(true)
		}
		b.WriteString(st.Render(string(r)))
	}
	return b.String()
}

// chip renders a small rounded-feeling badge.
func chip(text string, f, b color.Color, bold bool) string {
	st := lipgloss.NewStyle().Foreground(f).Background(b).Padding(0, 1)
	if bold {
		st = st.Bold(true)
	}
	return st.Render(text)
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// pad right-pads s (which may contain ANSI) to width w, truncating if needed.
func pad(s string, w int) string {
	s = truncate(s, w)
	if d := w - ansi.StringWidth(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

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

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/24/7))
	case d < 365*24*time.Hour:
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2006")
}

func relTimeLong(t time.Time) string {
	d := time.Since(t)
	unit := func(n int, s string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s ago", s)
		}
		return fmt.Sprintf("%d %ss ago", n, s)
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return unit(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return unit(int(d.Hours()), "hour")
	case d < 30*24*time.Hour:
		return unit(int(d.Hours()/24), "day")
	case d < 365*24*time.Hour:
		return unit(int(d.Hours()/24/30), "month")
	}
	return unit(int(d.Hours()/24/365), "year")
}

func initials(name string) string {
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

func hashIndex(s string) int {
	h := fnv.New32a()
	h.Write([]byte(s))
	return int(h.Sum32() & 0x7fffffff)
}

func (s *Styles) avatar(name string, bg color.Color) string {
	c := s.lane(hashIndex(name))
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

// Package themed builds Bubbles components styled from a tui.Palette, so
// inputs and spinners match the rest of the design language.
package themed

import (
	"image/color"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/karim-w/slop/pkg/tui"
)

// InputStyles styles a text input: Accent prompt and cursor, Primary text,
// Muted placeholder.
func InputStyles(p *tui.Palette) textinput.Styles {
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	s := textinput.DefaultStyles(p.T.Dark)
	s.Focused.Prompt = fg(p.Accent)
	s.Focused.Text = fg(p.Text)
	s.Focused.Placeholder = fg(p.Muted)
	s.Blurred.Prompt = fg(p.Muted)
	s.Blurred.Text = fg(p.Sub)
	s.Blurred.Placeholder = fg(p.Muted)
	s.Cursor.Color = p.Accent2
	return s
}

// Input returns a text input with prompt, width and palette styles.
func Input(p *tui.Palette, prompt string, width int) textinput.Model {
	in := textinput.New()
	in.Prompt = prompt
	in.SetWidth(width)
	in.SetStyles(InputStyles(p))
	return in
}

// Password is Input that hides what is typed.
func Password(p *tui.Palette, prompt string, width int) textinput.Model {
	in := Input(p, prompt, width)
	in.EchoMode = textinput.EchoPassword
	in.EchoCharacter = '•'
	return in
}

// Spinner returns a small Accent spinner.
func Spinner(p *tui.Palette) spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(p.Accent)))
}

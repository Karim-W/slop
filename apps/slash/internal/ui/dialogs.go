package ui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/tui/themed"
)

// modal is a dialog drawn over the browser. Update returns nil to close it.
type modal interface {
	Update(msg tea.Msg) (modal, tea.Cmd)
	View() tui.View
}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

type confirmDialog struct {
	title     string
	message   string
	danger    bool
	onConfirm tea.Cmd
	onCancel  tea.Cmd
}

func (d *confirmDialog) Update(msg tea.Msg) (modal, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	switch key.String() {
	case "y", "Y", "enter":
		return nil, d.onConfirm
	case "n", "N", "esc", "q":
		return nil, d.onCancel
	}
	return d, nil
}

func (d *confirmDialog) View() tui.View {
	return tui.Dialog(d.title, tui.Text(d.message).Wrap()).
		Width(62).
		Danger(d.danger).
		Hints(tui.H("y", "confirm"), tui.H("n", "cancel"))
}

type promptDialog struct {
	title    string
	input    textinput.Model
	onSubmit func(value string) tea.Cmd
}

func newPrompt(p *tui.Palette, title, initial string, onSubmit func(string) tea.Cmd) *promptDialog {
	input := themed.Input(p, "› ", 48)
	input.SetValue(initial)
	input.Focus()
	return &promptDialog{title: title, input: input, onSubmit: onSubmit}
}

func (d *promptDialog) Update(msg tea.Msg) (modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			return nil, nil
		case "enter":
			value := strings.TrimSpace(d.input.Value())
			if value == "" {
				return nil, nil
			}
			return nil, d.onSubmit(value)
		}
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return d, cmd
}

func (d *promptDialog) View() tui.View {
	return tui.Dialog(d.title, tui.Raw(d.input.View())).
		Hints(tui.H("enter", "ok"), tui.H("esc", "cancel"))
}

type helpDialog struct{}

var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"Navigate", [][2]string{
		{"↑ ↓ / j k", "move"},
		{"enter / l", "open folder"},
		{"backspace / h", "parent folder"},
		{"g / G", "top / bottom"},
		{"tab", "switch pane / tray"},
		{"~", "home folder"},
		{"/", "filter"},
		{".", "show hidden files"},
		{"ctrl+r", "refresh"},
	}},
	{"Files", [][2]string{
		{"space", "mark / unmark"},
		{"a", "mark all / none"},
		{"c / F5", "copy to other pane"},
		{"n / F7", "new folder"},
		{"r / F2", "rename"},
		{"d / F8", "delete"},
	}},
	{"Session", [][2]string{
		{"s", "site manager"},
		{"ctrl+d", "disconnect"},
		{"t", "expand / collapse tray"},
		{"q", "quit"},
	}},
	{"Tray (focused)", [][2]string{
		{"x / X", "cancel one / all"},
		{"r", "retry failed"},
		{"C", "clear finished"},
	}},
}

func (helpDialog) Update(msg tea.Msg) (modal, tea.Cmd) {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		return nil, nil
	}
	return helpDialog{}, nil
}

func (helpDialog) View() tui.View {
	section := func(i int) tui.View {
		s := helpSections[i]
		return tui.Width(40, tui.VStack(
			tui.Text(s.title).Fg(tui.Accent).Bold(),
			tui.Text(""),
			tui.ForEach(s.keys, func(_ int, k [2]string) tui.View {
				return tui.HStack(tui.Width(15, tui.Text(k[0]).Fg(tui.Accent).Bold()), tui.Text(k[1]))
			}),
		))
	}
	return tui.Dialog("Keys",
		tui.HStack(section(0), section(1)),
		tui.Text(""),
		tui.HStack(section(2), section(3)),
		tui.Text(""),
		tui.Text("press any key to close").Fg(tui.Muted),
	).Width(86)
}

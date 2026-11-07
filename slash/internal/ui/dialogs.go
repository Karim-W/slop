package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// modal is a dialog drawn over the browser. Update returns nil to close it.
type modal interface {
	Update(msg tea.Msg) (modal, tea.Cmd)
	View() string
}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

func dialogTitle(text string) string {
	return styleAccent.Bold(true).Render(text)
}

func keyHints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, styleKey.Render(pairs[i])+" "+styleMuted.Render(pairs[i+1]))
	}
	return strings.Join(parts, styleMuted.Render("  ·  "))
}

type confirmDialog struct {
	title     string
	message   string
	danger    bool
	onConfirm tea.Cmd
	onCancel  tea.Cmd
}

func (d *confirmDialog) Update(msg tea.Msg) (modal, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
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

func (d *confirmDialog) View() string {
	title := dialogTitle(d.title)
	if d.danger {
		title = styleError.Bold(true).Render(d.title)
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		styleText.Width(56).Render(d.message),
		"",
		keyHints("y", "confirm", "n", "cancel"),
	)
	style := styleModal
	if d.danger {
		style = style.BorderForeground(colorError)
	}
	return style.Render(body)
}

type promptDialog struct {
	title    string
	input    textinput.Model
	onSubmit func(value string) tea.Cmd
}

func newPrompt(title, initial string, onSubmit func(string) tea.Cmd) *promptDialog {
	input := textinput.New()
	input.SetValue(initial)
	input.Width = 48
	input.Prompt = "› "
	input.PromptStyle = styleAccent
	input.Focus()
	return &promptDialog{title: title, input: input, onSubmit: onSubmit}
}

func (d *promptDialog) Update(msg tea.Msg) (modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
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

func (d *promptDialog) View() string {
	return styleModal.Render(lipgloss.JoinVertical(lipgloss.Left,
		dialogTitle(d.title),
		"",
		d.input.View(),
		"",
		keyHints("enter", "ok", "esc", "cancel"),
	))
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
	if _, ok := msg.(tea.KeyMsg); ok {
		return nil, nil
	}
	return helpDialog{}, nil
}

func (helpDialog) View() string {
	var columns []string
	for _, section := range helpSections {
		lines := []string{dialogTitle(section.title), ""}
		for _, k := range section.keys {
			lines = append(lines, styleKey.Render(fit(k[0], 15))+styleText.Render(k[1]))
		}
		columns = append(columns, lipgloss.NewStyle().Width(40).Render(strings.Join(lines, "\n")))
	}
	top := lipgloss.JoinHorizontal(lipgloss.Top, columns[0], columns[1])
	bottom := lipgloss.JoinHorizontal(lipgloss.Top, columns[2], columns[3])
	return styleModal.Render(lipgloss.JoinVertical(lipgloss.Left,
		top, "", bottom, "", styleMuted.Render("press any key to close"),
	))
}

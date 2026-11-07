package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/karim-w/slop/slash/internal/config"
)

type connectRequestMsg struct {
	site     config.Site
	password string
}

type saveSiteMsg struct {
	site        config.Site
	newPassword string
	connect     bool
}

type deleteSiteMsg struct{ site config.Site }

type siteDeletionConfirmedMsg struct{ site config.Site }

type openSiteFormMsg struct{ site config.Site }

type openSitePickerMsg struct{}

// sitePicker lists saved sites and is the app's entry point.
type sitePicker struct {
	sites  []config.Site
	cursor int
}

func (p *sitePicker) Update(msg tea.Msg) (modal, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	site, hasSite := p.current()
	switch key.String() {
	case "esc", "q":
		return nil, nil
	case "up", "k":
		p.cursor = max(p.cursor-1, 0)
	case "down", "j":
		p.cursor = min(p.cursor+1, max(len(p.sites)-1, 0))
	case "n":
		return nil, emit(openSiteFormMsg{site: config.Site{Port: config.DefaultPort, Connections: config.DefaultConnections}})
	case "e":
		if hasSite {
			return nil, emit(openSiteFormMsg{site: site})
		}
	case "d", "delete":
		if hasSite {
			return nil, emit(deleteSiteMsg{site: site})
		}
	case "enter":
		if hasSite {
			return nil, emit(connectRequestMsg{site: site})
		}
		return nil, emit(openSiteFormMsg{site: config.Site{Port: config.DefaultPort, Connections: config.DefaultConnections}})
	}
	return p, nil
}

func (p *sitePicker) current() (config.Site, bool) {
	if p.cursor >= len(p.sites) {
		return config.Site{}, false
	}
	return p.sites[p.cursor], true
}

func (p *sitePicker) View() string {
	const width = 60
	lines := []string{dialogTitle("Site Manager"), ""}
	if len(p.sites) == 0 {
		lines = append(lines,
			styleText.Render("No saved sites yet."),
			styleMuted.Render("Press ")+styleKey.Render("n")+styleMuted.Render(" to add your first server."),
		)
	}
	for i, site := range p.sites {
		lines = append(lines, siteRow(site, width, i == p.cursor)...)
	}
	lines = append(lines, "", keyHints("enter", "connect", "n", "new", "e", "edit", "d", "delete", "esc", "close"))
	return styleModal.Render(strings.Join(lines, "\n"))
}

func siteRow(site config.Site, width int, selected bool) []string {
	pointer := "  "
	nameStyle := styleText
	if selected {
		pointer = styleAccent.Render("▍ ")
		nameStyle = styleBold
	}
	details := fmt.Sprintf("%s@%s · %d conn", site.User, site.Address(), site.Connections)
	if site.RememberPassword {
		details += " · remembered"
	}
	return []string{
		pointer + fit(nameStyle.Render(site.DisplayName()), width-2),
		pointer + styleMuted.Render(fit(details, width-2)),
	}
}

type formField int

const (
	fieldName formField = iota
	fieldHost
	fieldPort
	fieldUser
	fieldKeyPath
	fieldPassword
	fieldConnections
	fieldRemember
	fieldCount
)

var fieldLabels = [...]string{
	fieldName:        "Name",
	fieldHost:        "Host",
	fieldPort:        "Port",
	fieldUser:        "User",
	fieldKeyPath:     "Private key",
	fieldPassword:    "Password",
	fieldConnections: "Connections",
	fieldRemember:    "Remember password",
}

// siteForm creates or edits a site. A blank password keeps the stored one.
type siteForm struct {
	site     config.Site
	inputs   [fieldRemember]textinput.Model
	remember bool
	focus    formField
	err      string
}

func newSiteForm(site config.Site) *siteForm {
	f := &siteForm{site: site, remember: site.RememberPassword}
	values := [fieldRemember]string{
		fieldName:        site.Name,
		fieldHost:        site.Host,
		fieldPort:        strconv.Itoa(site.Port),
		fieldUser:        site.User,
		fieldKeyPath:     site.KeyPath,
		fieldConnections: strconv.Itoa(site.Connections),
	}
	placeholders := [fieldRemember]string{
		fieldName:        "optional label",
		fieldHost:        "example.com",
		fieldUser:        "root",
		fieldKeyPath:     "~/.ssh/id_ed25519 (optional, agent & defaults are tried)",
		fieldPassword:    "password or key passphrase",
		fieldConnections: "parallel transfer connections (1-16)",
	}
	if site.ID != "" {
		placeholders[fieldPassword] = "leave blank to keep current"
	}
	for i := range f.inputs {
		input := textinput.New()
		input.Prompt = ""
		input.Width = 40
		input.SetValue(values[i])
		input.Placeholder = placeholders[i]
		input.PlaceholderStyle = styleMuted
		f.inputs[i] = input
	}
	f.inputs[fieldPassword].EchoMode = textinput.EchoPassword
	f.inputs[fieldPassword].EchoCharacter = '•'
	f.setFocus(fieldHost)
	if site.Host != "" {
		f.setFocus(fieldName)
	}
	return f
}

func (f *siteForm) setFocus(field formField) {
	f.focus = (field + fieldCount) % fieldCount
	for i := range f.inputs {
		if formField(i) == f.focus {
			f.inputs[i].Focus()
		} else {
			f.inputs[i].Blur()
		}
	}
}

func (f *siteForm) Update(msg tea.Msg) (modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return nil, emit(openSitePickerMsg{})
		case "tab", "down":
			f.setFocus(f.focus + 1)
			return f, nil
		case "shift+tab", "up":
			f.setFocus(f.focus - 1)
			return f, nil
		case "enter", "ctrl+s":
			site, err := f.build()
			if err != nil {
				f.err = err.Error()
				return f, nil
			}
			password := f.inputs[fieldPassword].Value()
			return nil, emit(saveSiteMsg{site: site, newPassword: password, connect: key.String() == "enter"})
		case " ":
			if f.focus == fieldRemember {
				f.remember = !f.remember
				return f, nil
			}
		}
	}
	if f.focus == fieldRemember {
		return f, nil
	}
	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	return f, cmd
}

func (f *siteForm) build() (config.Site, error) {
	value := func(field formField) string { return strings.TrimSpace(f.inputs[field].Value()) }
	site := f.site
	site.Name = value(fieldName)
	site.Host = value(fieldHost)
	site.User = value(fieldUser)
	site.KeyPath = value(fieldKeyPath)
	site.RememberPassword = f.remember

	if site.Host == "" {
		return site, fmt.Errorf("host is required")
	}
	if site.User == "" {
		return site, fmt.Errorf("user is required")
	}
	port, err := strconv.Atoi(value(fieldPort))
	if err != nil || port < 1 || port > 65535 {
		return site, fmt.Errorf("port must be a number between 1 and 65535")
	}
	site.Port = port
	connections, err := strconv.Atoi(value(fieldConnections))
	if err != nil || connections < 1 || connections > config.MaxConnections {
		return site, fmt.Errorf("connections must be between 1 and %d", config.MaxConnections)
	}
	site.Connections = connections
	return site.WithDefaults(), nil
}

func (f *siteForm) View() string {
	title := "New Site"
	if f.site.ID != "" {
		title = "Edit Site"
	}
	lines := []string{dialogTitle(title), ""}
	for i := range f.inputs {
		lines = append(lines, f.fieldLine(formField(i), f.inputs[i].View()))
	}
	box := "[ ]"
	if f.remember {
		box = styleSuccess.Render("[✓]")
	}
	lines = append(lines, "", f.fieldLine(fieldRemember, box+styleMuted.Render("  stored in your OS keychain")))
	if f.err != "" {
		lines = append(lines, "", styleError.Render(f.err))
	}
	lines = append(lines, "", keyHints("enter", "save & connect", "ctrl+s", "save", "tab", "next", "esc", "back"))
	return styleModal.Render(strings.Join(lines, "\n"))
}

func (f *siteForm) fieldLine(field formField, value string) string {
	label := styleMuted.Render(fit(fieldLabels[field], 19))
	if field == f.focus {
		label = styleAccent.Render(fit("› "+fieldLabels[field], 19))
	}
	return label + value
}

// passwordPrompt asks for a password after key/agent authentication failed.
type passwordPrompt struct {
	site     config.Site
	input    textinput.Model
	remember bool
	retry    bool
}

func newPasswordPrompt(site config.Site, retry bool) *passwordPrompt {
	input := textinput.New()
	input.Prompt = "› "
	input.PromptStyle = styleAccent
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '•'
	input.Width = 40
	input.Focus()
	return &passwordPrompt{site: site, input: input, remember: site.RememberPassword, retry: retry}
}

func (p *passwordPrompt) Update(msg tea.Msg) (modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			return nil, emit(openSitePickerMsg{})
		case "tab", "ctrl+r":
			p.remember = !p.remember
			return p, nil
		case "enter":
			site := p.site
			site.RememberPassword = p.remember
			return nil, emit(connectRequestMsg{site: site, password: p.input.Value()})
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return p, cmd
}

func (p *passwordPrompt) View() string {
	notice := styleMuted.Render("Key and agent authentication were not accepted.")
	if p.retry {
		notice = styleError.Render("Authentication failed. Try again.")
	}
	box := "[ ]"
	if p.remember {
		box = styleSuccess.Render("[✓]")
	}
	return styleModal.Render(lipgloss.JoinVertical(lipgloss.Left,
		dialogTitle("Password for "+p.site.User+"@"+p.site.Host),
		"",
		notice,
		"",
		p.input.View(),
		"",
		box+styleText.Render(" Remember password"),
		"",
		keyHints("enter", "connect", "tab", "toggle remember", "esc", "cancel"),
	))
}

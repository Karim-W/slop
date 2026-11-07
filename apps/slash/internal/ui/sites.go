package ui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/slash/internal/config"
	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/tui/themed"
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
	key, ok := msg.(tea.KeyPressMsg)
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

func (p *sitePicker) View() tui.View {
	var body tui.View
	if len(p.sites) == 0 {
		body = tui.VStack(
			tui.Text("No saved sites yet."),
			tui.Line(
				tui.S("Press ", tui.Fg(tui.Muted)),
				tui.S("n", tui.Fg(tui.Accent).Bold()),
				tui.S(" to add your first server.", tui.Fg(tui.Muted)),
			),
		)
	} else {
		body = tui.ForEach(p.sites, func(i int, site config.Site) tui.View { return siteRow(site, i == p.cursor) })
	}
	return tui.Dialog("Site Manager", body).
		Width(66).
		Hints(tui.H("enter", "connect"), tui.H("n", "new"), tui.H("e", "edit"), tui.H("d", "delete"), tui.H("esc", "close"))
}

func siteRow(site config.Site, selected bool) tui.View {
	pointer := tui.Text("  ")
	name := tui.Text(site.DisplayName())
	if selected {
		pointer = tui.Text("▍ ").Fg(tui.Accent)
		name = name.Bold()
	}
	details := fmt.Sprintf("%s@%s · %d conn", site.User, site.Address(), site.Connections)
	if site.RememberPassword {
		details += " · remembered"
	}
	return tui.VStack(
		tui.HStack(pointer, name),
		tui.HStack(pointer, tui.Text(details).Fg(tui.Muted)),
	)
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

func newSiteForm(pal *tui.Palette, site config.Site) *siteForm {
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
		input := themed.Input(pal, "", 40)
		if formField(i) == fieldPassword {
			input = themed.Password(pal, "", 40)
		}
		input.SetValue(values[i])
		input.Placeholder = placeholders[i]
		f.inputs[i] = input
	}
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
	if key, ok := msg.(tea.KeyPressMsg); ok {
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
		case "space":
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

func (f *siteForm) View() tui.View {
	title := "New Site"
	if f.site.ID != "" {
		title = "Edit Site"
	}
	rows := make([]tui.View, 0, len(f.inputs)+4)
	for i := range f.inputs {
		rows = append(rows, f.fieldLine(formField(i), tui.Raw(f.inputs[i].View())))
	}
	rows = append(rows, tui.Text(""), f.fieldLine(fieldRemember, tui.HStack(
		tui.Checkbox(f.remember),
		tui.Text("  stored in your OS keychain").Fg(tui.Muted),
	)))
	if f.err != "" {
		rows = append(rows, tui.Text(""), tui.Text(f.err).Fg(tui.Danger))
	}
	return tui.Dialog(title, rows...).
		Width(70).
		Hints(tui.H("enter", "save & connect"), tui.H("ctrl+s", "save"), tui.H("tab", "next"), tui.H("esc", "back"))
}

func (f *siteForm) fieldLine(field formField, value tui.View) tui.View {
	label := tui.Text(fieldLabels[field]).Fg(tui.Muted)
	if field == f.focus {
		label = tui.Text("› " + fieldLabels[field]).Fg(tui.Accent)
	}
	return tui.HStack(tui.Width(19, label), value)
}

// passwordPrompt asks for a password after key/agent authentication failed.
type passwordPrompt struct {
	site     config.Site
	input    textinput.Model
	remember bool
	retry    bool
}

func newPasswordPrompt(pal *tui.Palette, site config.Site, retry bool) *passwordPrompt {
	input := themed.Password(pal, "› ", 40)
	input.Focus()
	return &passwordPrompt{site: site, input: input, remember: site.RememberPassword, retry: retry}
}

func (p *passwordPrompt) Update(msg tea.Msg) (modal, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
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

func (p *passwordPrompt) View() tui.View {
	notice := tui.Text("Key and agent authentication were not accepted.").Fg(tui.Muted)
	if p.retry {
		notice = tui.Text("Authentication failed. Try again.").Fg(tui.Danger)
	}
	return tui.Dialog("Password for "+p.site.User+"@"+p.site.Host,
		notice,
		tui.Text(""),
		tui.Raw(p.input.View()),
		tui.Text(""),
		tui.HStack(tui.Checkbox(p.remember), tui.Text(" Remember password")),
	).Hints(tui.H("enter", "connect"), tui.H("tab", "toggle remember"), tui.H("esc", "cancel"))
}

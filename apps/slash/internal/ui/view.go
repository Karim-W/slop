package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/utils/textx"
)

const (
	minWidth  = 60
	minHeight = 16
)

func (m *Model) View() tea.View {
	v := tea.NewView(tui.Render(m.pal, m.root(), m.width, m.height))
	v.AltScreen = true
	v.ReportFocus = true
	v.WindowTitle = "slash"
	if m.session != nil {
		v.WindowTitle = "slash · " + m.session.Label()
	}
	if !m.termMode {
		// Paint the terminal with the theme. In terminal mode the host's
		// own colours already are the theme.
		v.BackgroundColor = m.pal.Bg
		v.ForegroundColor = m.pal.Text
	}
	return v
}

// root is the whole screen:
//
//	header
//	local pane │ remote pane
//	transfer tray
//	footer
func (m *Model) root() tui.View {
	if m.width < minWidth || m.height < minHeight {
		return tui.Center(tui.Textf("Terminal too small (%d×%d). Need at least %d×%d.",
			m.width, m.height, minWidth, minHeight).Fg(tui.Muted).Wrap().Align(tui.Middle))
	}
	screen := tui.VStack(
		m.header(),
		tui.HStack(
			m.local.view(m.focus == focusLocal),
			m.remote.view(m.focus == focusRemote),
		).Grow(),
		m.tray.view(m.focus == focusTray),
		m.footer(),
	)
	var dialog tui.View
	if m.modal != nil {
		dialog = m.modal.View()
	}
	return tui.Modal(screen, dialog)
}

func (m *Model) header() tui.View {
	return tui.HStack(
		tui.Gradient(" ⟋ slash ").Bold(),
		tui.Spacer(),
		m.connectionStatus(),
		tui.Text(" "),
	)
}

func (m *Model) connectionStatus() tui.View {
	switch {
	case m.connecting != "":
		return tui.HStack(tui.Raw(m.spinner.View()), tui.Text(" connecting to "+m.connecting+"…").Fg(tui.Muted))
	case m.session != nil:
		return tui.Line(
			tui.S("●", tui.Fg(tui.Success)),
			tui.S(" "+m.session.Label(), tui.Fg(tui.Primary)),
			tui.S(fmt.Sprintf("  ·  %d connections", m.transfers.Connections()), tui.Fg(tui.Muted)),
		)
	}
	return tui.Text("○ offline").Fg(tui.Muted)
}

func (m *Model) footer() tui.View {
	var body tui.View
	switch {
	case m.filtering:
		body = tui.Raw(m.filterBox.View())
	case m.flash.text != "":
		body = m.flashView()
	default:
		body = m.hints()
	}
	return tui.Pad(body, 0, 1)
}

func (m *Model) flashView() tui.View {
	color, icon := tui.Color(tui.Accent), "•"
	switch m.flash.kind {
	case flashSuccess:
		color, icon = tui.Success, "✓"
	case flashError:
		color, icon = tui.Danger, "✗"
	}
	return tui.Text(icon + " " + textx.FirstLine(m.flash.text)).Fg(color)
}

func (m *Model) hints() tui.View {
	if m.focus == focusTray {
		return tui.KeyHints(tui.H("↑↓", "select"), tui.H("x", "cancel"), tui.H("X", "cancel all"),
			tui.H("r", "retry"), tui.H("C", "clear done"), tui.H("tab", "back"))
	}
	return tui.KeyHints(tui.H("tab", "switch"), tui.H("space", "mark"), tui.H("c", "copy"),
		tui.H("n", "mkdir"), tui.H("r", "rename"), tui.H("d", "delete"), tui.H("t", "tray"),
		tui.H("s", "sites"), tui.H("?", "help"))
}

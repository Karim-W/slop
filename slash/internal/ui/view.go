package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	minWidth  = 60
	minHeight = 16
)

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			styleMuted.Render(fmt.Sprintf("Terminal too small (%d×%d). Need at least %d×%d.", m.width, m.height, minWidth, minHeight)))
	}

	screen := lipgloss.JoinVertical(lipgloss.Left,
		m.headerView(),
		m.panesView(),
		m.tray.view(m.width, m.focus == focusTray),
		m.footerView(),
	)
	if m.modal == nil {
		return screen
	}
	return overlay(dimAll(screen), m.modal.View(), m.width, m.height)
}

func (m *Model) panesView() string {
	height := m.height - 2 - m.tray.height()
	leftWidth := m.width / 2
	rightWidth := m.width - leftWidth
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.local.view(leftWidth, height, m.focus == focusLocal),
		m.remote.view(rightWidth, height, m.focus == focusRemote),
	)
}

func (m *Model) headerView() string {
	brand := styleBrand.Render("⟋ slash")
	status := m.connectionStatus()
	gap := max(m.width-lipgloss.Width(brand)-lipgloss.Width(status)-1, 1)
	return brand + strings.Repeat(" ", gap) + status + " "
}

func (m *Model) connectionStatus() string {
	switch {
	case m.connecting != "":
		return m.spinner.View() + styleMuted.Render(" connecting to "+m.connecting+"…")
	case m.session != nil:
		return styleSuccess.Render("●") + styleText.Render(" "+m.session.Label()) +
			styleMuted.Render(fmt.Sprintf("  ·  %d connections", m.transfers.Connections()))
	default:
		return styleMuted.Render("○ offline")
	}
}

func (m *Model) footerView() string {
	if m.filtering {
		return " " + m.filterBox.View()
	}
	if m.flash.text != "" {
		return " " + m.flashView()
	}
	return " " + m.hintsView()
}

func (m *Model) flashView() string {
	style, icon := styleAccent, "•"
	switch m.flash.kind {
	case flashSuccess:
		style, icon = styleSuccess, "✓"
	case flashError:
		style, icon = styleError, "✗"
	}
	return fit(style.Render(icon+" "+firstLine(m.flash.text)), m.width-2)
}

func (m *Model) hintsView() string {
	var hints string
	if m.focus == focusTray {
		hints = keyHints("↑↓", "select", "x", "cancel", "X", "cancel all", "r", "retry", "C", "clear done", "tab", "back")
	} else {
		hints = keyHints("tab", "switch", "space", "mark", "c", "copy", "n", "mkdir", "r", "rename", "d", "delete", "t", "tray", "s", "sites", "?", "help")
	}
	return fit(hints, m.width-2)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

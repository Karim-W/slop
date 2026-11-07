package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/karim-w/slop/slash/internal/config"
	"github.com/karim-w/slop/slash/internal/session"
	"github.com/karim-w/slop/slash/internal/vfs"
)

const trayRefreshInterval = 250 * time.Millisecond

type listedMsg struct {
	side      side
	path      string
	entries   []vfs.Entry
	focusName string
	err       error
}

type connectedMsg struct {
	session  *session.Session
	home     string
	password string
}

type connectFailedMsg struct {
	site     config.Site
	password string
	err      error
}

type operationDoneMsg struct {
	side      side
	focusName string
	success   string
	err       error
}

type trustHostMsg struct {
	unknown  *session.UnknownHostError
	site     config.Site
	password string
}

type tickMsg time.Time

type clearFlashMsg struct{ id int }

func listDir(s side, fsys vfs.FileSystem, path, focusName string) tea.Cmd {
	return func() tea.Msg {
		entries, err := fsys.List(path)
		return listedMsg{side: s, path: path, entries: entries, focusName: focusName, err: err}
	}
}

func connect(site config.Site, password string) tea.Cmd {
	return func() tea.Msg {
		sess, err := session.Open(site, password)
		if err != nil {
			return connectFailedMsg{site: site, password: password, err: err}
		}
		home, err := sess.Browse.Getwd()
		if err != nil {
			home = "/"
		}
		return connectedMsg{session: sess, home: home, password: password}
	}
}

func trustHostAndConnect(unknown *session.UnknownHostError, site config.Site, password string) tea.Cmd {
	return func() tea.Msg {
		if err := session.TrustHost(unknown); err != nil {
			return connectFailedMsg{site: site, password: password, err: err}
		}
		return connect(site, password)()
	}
}

// fileOperation runs a filesystem change off the UI goroutine, then reports
// which pane should be refreshed and where its cursor should land.
func fileOperation(s side, focusName, success string, run func() error) tea.Cmd {
	return func() tea.Msg {
		return operationDoneMsg{side: s, focusName: focusName, success: success, err: run()}
	}
}

func rememberPassword(site config.Site, password string) tea.Cmd {
	return func() tea.Msg {
		if !site.RememberPassword {
			_ = config.ForgetPassword(site.ID)
			return nil
		}
		if password == "" {
			return nil
		}
		if err := config.SavePassword(site.ID, password); err != nil {
			return flashMsg{text: "Couldn't save password to keychain: " + err.Error(), kind: flashError}
		}
		return nil
	}
}

func tick() tea.Cmd {
	return tea.Tick(trayRefreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

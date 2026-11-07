// Package ui is the Bubble Tea front end: two file panes, a transfer tray
// and a handful of modal dialogs layered on top.
package ui

import (
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/karim-w/slop/slash/internal/config"
	"github.com/karim-w/slop/slash/internal/session"
	"github.com/karim-w/slop/slash/internal/transfer"
	"github.com/karim-w/slop/slash/internal/vfs"
)

type focusArea int

const (
	focusLocal focusArea = iota
	focusRemote
	focusTray
)

type flashKind int

const (
	flashInfo flashKind = iota
	flashSuccess
	flashError
)

type flashMsg struct {
	text string
	kind flashKind
}

const flashDuration = 4 * time.Second

type Model struct {
	store *config.Store

	local  pane
	remote pane
	tray   tray
	focus  focusArea

	session         *session.Session
	transfers       *transfer.Manager
	seenCompletions uint64

	modal      modal
	filtering  bool
	filterBox  textinput.Model
	connecting string
	spinner    spinner.Model

	flash   flashMsg
	flashID int

	width, height int
}

func New(store *config.Store) *Model {
	filterBox := textinput.New()
	filterBox.Prompt = "/ "
	filterBox.PromptStyle = styleWarn

	m := &Model{
		store:     store,
		local:     newPane(localSide),
		remote:    newPane(remoteSide),
		filterBox: filterBox,
		spinner:   spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(styleAccent)),
	}
	m.local.fs = vfs.NewLocal()
	m.modal = m.newSitePicker()
	return m
}

func (m *Model) Init() tea.Cmd {
	home, err := m.local.fs.Home()
	if err != nil {
		home = "/"
	}
	return tea.Batch(listDir(localSide, m.local.fs, home, ""), tick())
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, m.quit()
		}
		if m.modal != nil {
			return m, m.updateModal(msg)
		}
		if m.filtering {
			return m, m.updateFilter(msg)
		}
		return m, m.handleKey(msg)

	case tickMsg:
		m.refreshTray(time.Time(msg))
		return m, tea.Batch(tick(), m.refreshAfterCompletions())

	case spinner.TickMsg:
		if m.connecting == "" {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case listedMsg:
		return m, m.applyListing(msg)

	case connectRequestMsg:
		return m, m.startConnect(msg.site, msg.password)

	case connectedMsg:
		return m, m.finishConnect(msg)

	case connectFailedMsg:
		return m, m.handleConnectFailure(msg)

	case saveSiteMsg:
		return m, m.saveSite(msg)

	case deleteSiteMsg:
		m.confirmDeleteSite(msg.site)
		return m, nil

	case siteDeletionConfirmedMsg:
		return m, m.deleteSite(msg.site)

	case trustHostMsg:
		m.connecting = msg.site.DisplayName()
		return m, tea.Batch(trustHostAndConnect(msg.unknown, msg.site, msg.password), m.spinner.Tick)

	case openSiteFormMsg:
		m.modal = newSiteForm(msg.site)
		return m, nil

	case openSitePickerMsg:
		m.modal = m.newSitePicker()
		return m, nil

	case operationDoneMsg:
		return m, m.finishOperation(msg)

	case quitMsg:
		return m, m.quit()

	case flashMsg:
		return m, m.showFlash(msg.text, msg.kind)

	case clearFlashMsg:
		if msg.id == m.flashID {
			m.flash = flashMsg{}
		}
		return m, nil
	}

	if m.modal != nil {
		return m, m.updateModal(msg)
	}
	return m, nil
}

func (m *Model) updateModal(msg tea.Msg) tea.Cmd {
	next, cmd := m.modal.Update(msg)
	m.modal = next
	return cmd
}

func (m *Model) showFlash(text string, kind flashKind) tea.Cmd {
	m.flashID++
	id := m.flashID
	m.flash = flashMsg{text: text, kind: kind}
	return tea.Tick(flashDuration, func(time.Time) tea.Msg { return clearFlashMsg{id: id} })
}

func (m *Model) showError(err error) tea.Cmd {
	return m.showFlash(err.Error(), flashError)
}

func (m *Model) pane(s side) *pane {
	if s == localSide {
		return &m.local
	}
	return &m.remote
}

func (m *Model) applyListing(msg listedMsg) tea.Cmd {
	p := m.pane(msg.side)
	if !p.connected() {
		return nil
	}
	if msg.err != nil {
		return m.handleListingError(p, msg)
	}
	p.setListing(msg.path, msg.entries, msg.focusName)
	if msg.side == remoteSide && m.session != nil {
		m.session.Site.LastRemoteDir = msg.path
	}
	return nil
}

// handleListingError keeps the pane where it was. On first load, when there
// is nowhere to stay, it falls back to the home folder (the last visited
// remote folder may have been deleted since).
func (m *Model) handleListingError(p *pane, msg listedMsg) tea.Cmd {
	p.loading = false
	if p.path != "" {
		return m.showError(msg.err)
	}
	home, err := p.fs.Home()
	if err == nil && home != msg.path {
		return m.navigate(p, home, "")
	}
	p.path = msg.path
	p.err = msg.err
	return nil
}

func (m *Model) navigate(p *pane, path, focusName string) tea.Cmd {
	if !p.connected() {
		return nil
	}
	p.loading = true
	return listDir(p.side, p.fs, path, focusName)
}

func (m *Model) reload(p *pane) tea.Cmd {
	return m.navigate(p, p.path, p.currentName())
}

func (m *Model) refreshTray(now time.Time) {
	if m.transfers == nil {
		return
	}
	m.tray.update(m.transfers.Stats(), now)
}

// refreshAfterCompletions reloads both panes whenever new files finished
// transferring, so freshly copied files show up without a manual refresh.
func (m *Model) refreshAfterCompletions() tea.Cmd {
	if m.transfers == nil {
		return nil
	}
	completed := m.transfers.Stats().Completed
	if completed == m.seenCompletions {
		return nil
	}
	m.seenCompletions = completed
	return tea.Batch(m.reload(&m.local), m.reload(&m.remote))
}

func (m *Model) newSitePicker() *sitePicker {
	return &sitePicker{sites: m.store.Sites}
}

func (m *Model) startConnect(site config.Site, password string) tea.Cmd {
	if password == "" && site.RememberPassword {
		password, _ = config.LoadPassword(site.ID)
	}
	m.connecting = site.DisplayName()
	return tea.Batch(connect(site, password), m.spinner.Tick)
}

func (m *Model) finishConnect(msg connectedMsg) tea.Cmd {
	m.connecting = ""
	m.disconnect()

	sess := msg.session
	m.session = sess
	workers := make([]vfs.FileSystem, len(sess.Workers))
	for i, client := range sess.Workers {
		workers[i] = vfs.NewRemote(client, sess.Label())
	}
	m.remote = newPane(remoteSide)
	m.remote.fs = vfs.NewRemote(sess.Browse, sess.Label())
	m.transfers = transfer.NewManager(m.local.fs, m.remote.fs, workers)
	m.tray = tray{expanded: m.tray.expanded}
	m.seenCompletions = 0
	m.focus = focusRemote

	start := msg.home
	if sess.Site.LastRemoteDir != "" {
		start = sess.Site.LastRemoteDir
	}
	connected := fmt.Sprintf("Connected to %s with %d transfer connections", sess.Label(), m.transfers.Connections())
	return tea.Batch(
		m.navigate(&m.remote, start, ""),
		rememberPassword(sess.Site, msg.password),
		m.showFlash(connected, flashSuccess),
	)
}

func (m *Model) handleConnectFailure(msg connectFailedMsg) tea.Cmd {
	m.connecting = ""
	var unknown *session.UnknownHostError
	switch {
	case errors.As(msg.err, &unknown):
		m.modal = &confirmDialog{
			title: "Unknown host",
			message: fmt.Sprintf("The authenticity of %s can't be established.\n\nFingerprint: %s\n\nTrust this host and add it to known_hosts?",
				unknown.Host, unknown.Fingerprint()),
			onConfirm: emit(trustHostMsg{unknown: unknown, site: msg.site, password: msg.password}),
			onCancel:  emit(openSitePickerMsg{}),
		}
		return nil
	case errors.Is(msg.err, session.ErrAuthFailed):
		m.modal = newPasswordPrompt(msg.site, msg.password != "")
		return nil
	default:
		m.modal = m.newSitePicker()
		return m.showError(fmt.Errorf("connecting to %s: %w", msg.site.DisplayName(), msg.err))
	}
}

func (m *Model) saveSite(msg saveSiteMsg) tea.Cmd {
	m.store.Upsert(msg.site)
	if err := m.store.Save(); err != nil {
		return m.showError(err)
	}
	cmds := []tea.Cmd{rememberPassword(msg.site, msg.newPassword)}
	if msg.connect {
		cmds = append(cmds, m.startConnect(msg.site, msg.newPassword))
	} else {
		m.modal = m.newSitePicker()
		cmds = append(cmds, m.showFlash("Saved "+msg.site.DisplayName(), flashSuccess))
	}
	return tea.Batch(cmds...)
}

func (m *Model) confirmDeleteSite(site config.Site) {
	m.modal = &confirmDialog{
		title:     "Delete site",
		message:   fmt.Sprintf("Remove %q and its saved password?", site.DisplayName()),
		danger:    true,
		onConfirm: emit(siteDeletionConfirmedMsg{site: site}),
		onCancel:  emit(openSitePickerMsg{}),
	}
}

func (m *Model) deleteSite(site config.Site) tea.Cmd {
	m.store.Delete(site.ID)
	m.modal = m.newSitePicker()
	if err := m.store.Save(); err != nil {
		return m.showError(err)
	}
	return func() tea.Msg {
		_ = config.ForgetPassword(site.ID)
		return nil
	}
}

func (m *Model) finishOperation(msg operationDoneMsg) tea.Cmd {
	p := m.pane(msg.side)
	reload := m.navigate(p, p.path, msg.focusName)
	if msg.err != nil {
		return tea.Batch(reload, m.showError(msg.err))
	}
	return tea.Batch(reload, m.showFlash(msg.success, flashSuccess))
}

// disconnect tears down the current session, remembering where the user was.
func (m *Model) disconnect() {
	if m.transfers != nil {
		m.transfers.Close()
		m.transfers = nil
	}
	if m.session != nil {
		m.store.Upsert(m.session.Site)
		_ = m.store.Save()
		m.session.Close()
		m.session = nil
	}
	m.remote = newPane(remoteSide)
	m.tray = tray{expanded: m.tray.expanded}
	if m.focus != focusLocal {
		m.focus = focusLocal
	}
}

func (m *Model) quit() tea.Cmd {
	m.disconnect()
	return tea.Quit
}

func (m *Model) hasActiveTransfers() bool {
	return m.tray.summary.active+m.tray.summary.queued > 0
}

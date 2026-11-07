package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/slash/internal/transfer"
	"github.com/karim-w/slop/apps/slash/internal/vfs"
	"github.com/karim-w/slop/pkg/utils/textx"
)

const pageSize = 10

var errNotConnected = errors.New("not connected: press s to open a site")

func (m *Model) focusedPane() *pane {
	if m.focus == focusRemote {
		return &m.remote
	}
	return &m.local
}

func (m *Model) otherPane(p *pane) *pane {
	if p.side == localSide {
		return &m.remote
	}
	return &m.local
}

func (m *Model) handleKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "q":
		return m.confirmQuit()
	case "?":
		m.modal = helpDialog{}
		return nil
	case "s":
		m.modal = m.newSitePicker()
		return nil
	case "ctrl+d":
		if m.session == nil {
			return nil
		}
		m.disconnect()
		return m.showFlash("Disconnected", flashInfo)
	case "t":
		m.tray.expanded = !m.tray.expanded
		if !m.tray.expanded && m.focus == focusTray {
			m.focus = focusLocal
		}
		return nil
	case "tab":
		m.cycleFocus()
		return nil
	}

	if m.focus == focusTray {
		return m.handleTrayKey(key)
	}
	return m.handlePaneKey(m.focusedPane(), key)
}

func (m *Model) cycleFocus() {
	switch m.focus {
	case focusLocal:
		m.focus = focusRemote
	case focusRemote:
		if m.tray.expanded && len(m.tray.items) > 0 {
			m.focus = focusTray
		} else {
			m.focus = focusLocal
		}
	case focusTray:
		m.focus = focusLocal
	}
}

func (m *Model) handlePaneKey(p *pane, key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "up", "k":
		p.move(-1)
	case "down", "j":
		p.move(1)
	case "pgup", "ctrl+u":
		p.move(-pageSize)
	case "pgdown", "ctrl+f":
		p.move(pageSize)
	case "home", "g":
		p.move(-len(p.visible))
	case "end", "G":
		p.move(len(p.visible))
	case "left":
		m.focus = focusLocal
	case "right":
		m.focus = focusRemote
	case "enter", "l":
		return m.open(p)
	case "backspace", "h":
		return m.goToParent(p)
	case "~":
		return m.goHome(p)
	case "space":
		p.toggleMark()
		p.move(1)
	case "a":
		p.toggleMarkAll()
	case ".":
		p.showHidden = !p.showHidden
		p.rebuildVisible()
	case "/":
		m.startFilter(p)
	case "esc":
		p.marked = map[string]bool{}
		p.filter = ""
		p.rebuildVisible()
	case "ctrl+r":
		return m.reload(p)
	case "c", "f5":
		return m.copySelection(p)
	case "n", "f7":
		return m.promptNewFolder(p)
	case "r", "f2":
		return m.promptRename(p)
	case "d", "delete", "f8":
		return m.confirmDelete(p)
	}
	return nil
}

func (m *Model) open(p *pane) tea.Cmd {
	entry, ok := p.current()
	if !ok || !entry.IsDir {
		return nil
	}
	if entry.Name == parentEntryName {
		return m.goToParent(p)
	}
	return m.navigate(p, p.fs.Join(p.path, entry.Name), "")
}

func (m *Model) goToParent(p *pane) tea.Cmd {
	if !p.connected() {
		return nil
	}
	return m.navigate(p, p.fs.Parent(p.path), p.fs.Base(p.path))
}

func (m *Model) goHome(p *pane) tea.Cmd {
	if !p.connected() {
		return nil
	}
	home, err := p.fs.Home()
	if err != nil {
		return m.showError(err)
	}
	return m.navigate(p, home, "")
}

func (m *Model) startFilter(p *pane) {
	if !p.connected() {
		return
	}
	m.filtering = true
	m.filterBox.SetValue(p.filter)
	m.filterBox.CursorEnd()
	m.filterBox.Focus()
}

func (m *Model) updateFilter(key tea.KeyPressMsg) tea.Cmd {
	p := m.focusedPane()
	switch key.String() {
	case "esc":
		p.filter = ""
		m.filtering = false
	case "enter":
		m.filtering = false
	default:
		var cmd tea.Cmd
		m.filterBox, cmd = m.filterBox.Update(key)
		p.filter = m.filterBox.Value()
		p.cursor = 0
		p.rebuildVisible()
		return cmd
	}
	m.filterBox.Blur()
	p.rebuildVisible()
	return nil
}

func (m *Model) copySelection(p *pane) tea.Cmd {
	if m.transfers == nil {
		return m.showError(errNotConnected)
	}
	selection := p.selection()
	if len(selection) == 0 {
		return nil
	}
	target := m.otherPane(p)
	direction := transfer.Upload
	if p.side == remoteSide {
		direction = transfer.Download
	}

	sources := make([]string, len(selection))
	var conflicts []string
	for i, entry := range selection {
		sources[i] = p.fs.Join(p.path, entry.Name)
		if target.has(entry.Name) {
			conflicts = append(conflicts, entry.Name)
		}
	}

	manager := m.transfers
	request := transfer.Request{Direction: direction, Sources: sources, DestDir: target.path}
	start := func() tea.Msg {
		manager.Enqueue(request)
		return flashMsg{text: fmt.Sprintf("Queued %s → %s", describe(selection), target.path), kind: flashInfo}
	}
	p.marked = map[string]bool{}

	if len(conflicts) == 0 {
		return start
	}
	m.modal = &confirmDialog{
		title:     "Overwrite?",
		message:   fmt.Sprintf("%s in %s:\n\n%s", textx.Pluralize(len(conflicts), "item already exists", "items already exist"), target.path, previewNames(conflicts)),
		danger:    true,
		onConfirm: start,
	}
	return nil
}

func (m *Model) promptNewFolder(p *pane) tea.Cmd {
	if !p.connected() {
		return m.showError(errNotConnected)
	}
	fsys, dir := p.fs, p.path
	m.modal = newPrompt(m.pal, "New folder in "+dir, "", func(name string) tea.Cmd {
		path := fsys.Join(dir, name)
		return fileOperation(p.side, name, "Created "+name, func() error { return fsys.Mkdir(path) })
	})
	return nil
}

func (m *Model) promptRename(p *pane) tea.Cmd {
	entry, ok := p.current()
	if !ok || entry.Name == parentEntryName {
		return nil
	}
	fsys, dir := p.fs, p.path
	m.modal = newPrompt(m.pal, "Rename "+entry.Name, entry.Name, func(name string) tea.Cmd {
		if name == entry.Name {
			return nil
		}
		from, to := fsys.Join(dir, entry.Name), fsys.Join(dir, name)
		return fileOperation(p.side, name, "Renamed to "+name, func() error { return fsys.Rename(from, to) })
	})
	return nil
}

func (m *Model) confirmDelete(p *pane) tea.Cmd {
	selection := p.selection()
	if len(selection) == 0 {
		return nil
	}
	fsys, dir := p.fs, p.path
	names := make([]string, len(selection))
	for i, entry := range selection {
		names[i] = entry.Name
	}
	m.modal = &confirmDialog{
		title:   "Delete from " + fsys.Label(),
		message: fmt.Sprintf("Permanently delete %s? This cannot be undone.\n\n%s", describe(selection), previewNames(names)),
		danger:  true,
		onConfirm: fileOperation(p.side, "", "Deleted "+describe(selection), func() error {
			return deleteAll(fsys, dir, names)
		}),
	}
	p.marked = map[string]bool{}
	return nil
}

func deleteAll(fsys vfs.FileSystem, dir string, names []string) error {
	var errs []error
	for _, name := range names {
		if err := fsys.RemoveAll(fsys.Join(dir, name)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func (m *Model) handleTrayKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.String() {
	case "up", "k":
		m.tray.move(-1)
	case "down", "j":
		m.tray.move(1)
	case "esc":
		m.focus = focusLocal
	case "x":
		if v, ok := m.tray.selected(); ok {
			m.transfers.Cancel(v.ID)
		}
	case "X":
		m.transfers.CancelAll()
	case "r":
		if v, ok := m.tray.selected(); ok {
			m.transfers.Retry(v.ID)
		}
	case "C":
		m.transfers.ClearFinished()
	}
	return nil
}

func (m *Model) confirmQuit() tea.Cmd {
	if !m.hasActiveTransfers() {
		return m.quit()
	}
	m.modal = &confirmDialog{
		title:     "Quit slash?",
		message:   "Transfers are still running. Quitting will cancel them.",
		danger:    true,
		onConfirm: emit(quitMsg{}),
	}
	return nil
}

type quitMsg struct{}

func describe(entries []vfs.Entry) string {
	if len(entries) == 1 {
		return entries[0].Name
	}
	return textx.Pluralize(len(entries), "item", "items")
}

func previewNames(names []string) string {
	const limit = 5
	shown := names[:min(len(names), limit)]
	preview := "  " + strings.Join(shown, "\n  ")
	if len(names) > limit {
		preview += fmt.Sprintf("\n  …and %d more", len(names)-limit)
	}
	return preview
}

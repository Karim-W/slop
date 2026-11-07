package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/kyro/internal/art"
	"github.com/karim-w/slop/apps/kyro/internal/home"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/apps/kyro/internal/player"
	"github.com/karim-w/slop/apps/kyro/internal/playlist"
	"github.com/karim-w/slop/pkg/infra/paths"
	"github.com/karim-w/slop/pkg/tui"
)

// sidebar entries: the library sections, then playlists, then "new".
type sideItem struct {
	sec      section
	playlist string
	create   bool
}

func (m *Model) sideItems() []sideItem {
	items := []sideItem{{sec: secSongs}, {sec: secAlbums}, {sec: secArtists}, {sec: secQueue}, {sec: secLoved}}
	for _, p := range m.playlists {
		items = append(items, sideItem{sec: secPlaylist, playlist: p.Name})
	}
	return append(items, sideItem{create: true})
}

func (m *Model) open(it sideItem) {
	m.sec, m.playlist, m.drill, m.cursor = it.sec, it.playlist, nil, 0
	m.scroll = tui.Scroll{}
	m.rebuild()
}

// cycleSection moves through the sidebar entries (compact layouts have no
// sidebar).
func (m *Model) cycleSection(d int) {
	items := m.sideItems()
	items = items[:len(items)-1] // skip "new playlist"
	cur := slices.IndexFunc(items, func(it sideItem) bool { return it.sec == m.sec && it.playlist == m.playlist })
	cur = (cur + d + len(items)) % len(items)
	m.side = cur
	m.open(items[cur])
}

func (m *Model) move(d int) {
	if m.focus == focusSidebar && m.layout() == layoutFull {
		m.side = min(max(m.side+d, 0), len(m.sideItems())-1)
		return
	}
	m.cursor = min(max(m.cursor+d, 0), max(len(m.rows)-1, 0))
}

// key handles a key press outside dialogs and the filter box.
func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	s := m.pl.State()
	switch k.String() {
	// ── playback, everywhere ──
	case "space":
		return m.fail(m.pl.Toggle())
	case "]", ">", "ctrl+n":
		return m.fail(m.pl.Next())
	case "[", "<", "ctrl+p":
		return m.fail(m.pl.Prev())
	case "right":
		return m.fail(m.pl.SeekBy(5 * time.Second))
	case "left":
		return m.fail(m.pl.SeekBy(-5 * time.Second))
	case "shift+right":
		return m.fail(m.pl.SeekBy(30 * time.Second))
	case "shift+left":
		return m.fail(m.pl.SeekBy(-30 * time.Second))
	case "+", "=":
		return m.fail(m.pl.SetVolume(s.Volume + 5))
	case "-", "_":
		return m.fail(m.pl.SetVolume(s.Volume - 5))
	case "s":
		m.pl.ToggleShuffle()
		if m.sec == secQueue {
			m.rebuild()
		}
		return m.showFlash(map[bool]string{true: "Shuffle on", false: "Shuffle off"}[m.pl.State().Shuffle], true)
	case "r":
		m.pl.CycleRepeat()
		return m.showFlash("Repeat "+string(m.pl.State().Repeat), true)
	case "z":
		m.zen = !m.zen
		return nil
	case "T":
		m.modal = newThemePicker(m)
		return nil
	case "c":
		return m.toggleDynamic()
	case "m":
		m.modal = newSoundPicker(m)
		return nil
	case "C":
		m.modal = newArtPicker(m)
		return m.artProbe()
	case "?":
		m.modal = helpDialog{}
		return nil
	case "q":
		return m.quit()
	}
	if cmd, ok := m.rateKey(k.String()); ok {
		return cmd
	}
	if m.zen {
		switch k.String() {
		case "a":
			if s := m.pl.State(); s.Active {
				m.modal = newPlaylistPicker(m, []library.Track{s.Track})
			}
		case "esc":
			m.zen = false
		}
		return nil
	}

	// ── browsing ──
	switch k.String() {
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup", "ctrl+u":
		m.move(-10)
	case "pgdown", "ctrl+d":
		m.move(10)
	case "home", "g":
		m.move(-1 << 20)
	case "end", "G":
		m.move(1 << 20)
	case "tab":
		if m.layout() == layoutFull {
			m.focus = 1 - m.focus
		} else {
			m.cycleSection(1)
		}
	case "shift+tab":
		if m.layout() == layoutFull {
			m.focus = 1 - m.focus
		} else {
			m.cycleSection(-1)
		}
	case "1", "2", "3", "4", "5":
		m.focus = focusList
		m.side = int(k.String()[0] - '1')
		m.open(m.sideItems()[m.side])
	case "/":
		m.filtering = true
		m.focus = focusList
		return m.filter.Focus()
	case "esc", "backspace":
		switch {
		case m.filter.Value() != "":
			m.filter.SetValue("")
			m.rebuild()
		case m.drill != nil:
			m.drill = nil
			m.rebuild()
		case m.focus == focusList && m.layout() == layoutFull:
			m.focus = focusSidebar
		}
	case "enter":
		return m.enter()
	case "e":
		for _, t := range m.selectedTracks() {
			m.pl.Enqueue(t)
		}
		m.rebuildIf(secQueue)
		return m.showFlash(fmt.Sprintf("Queued %s", describe(m.selectedTracks())), true)
	case "E":
		ts := m.selectedTracks()
		for i := len(ts) - 1; i >= 0; i-- {
			m.pl.PlayNext(ts[i])
		}
		m.rebuildIf(secQueue)
		return m.showFlash("Playing next: "+describe(ts), true)
	case "a":
		if ts := m.selectedTracks(); len(ts) > 0 {
			m.modal = newPlaylistPicker(m, ts)
		}
	case "A":
		var ts []library.Track
		for _, r := range m.rows {
			if r.track != nil {
				ts = append(ts, *r.track)
			} else {
				ts = append(ts, r.tracks...)
			}
		}
		if len(ts) > 0 {
			m.modal = newPlaylistPicker(m, ts)
		}
	case "N":
		m.modal = newPrompt(m, "New playlist", "", func(name string) tea.Cmd { return m.createPlaylist(name, nil) })
	case "i":
		m.modal = newPrompt(m, "Import music (a folder or file)", "~/Music", func(p string) tea.Cmd { return m.importSource(p) })
	case "ctrl+r":
		return m.rescan()
	case "x", "delete":
		return m.removeFromPlaylist()
	case "K", "shift+up":
		return m.moveInPlaylist(-1)
	case "J", "shift+down":
		return m.moveInPlaylist(1)
	case "R":
		if name := m.currentPlaylist(); name != "" {
			m.modal = newPrompt(m, "Rename "+name, name, func(n string) tea.Cmd {
				if err := m.d.Playlists.Rename(name, n); err != nil {
					return m.fail(err)
				}
				m.reloadPlaylists()
				if m.playlist == name {
					m.playlist = n
				}
				return m.showFlash("Renamed to "+n, true)
			})
		}
	case "D":
		if name := m.currentPlaylist(); name != "" {
			m.modal = newConfirm("Delete playlist", fmt.Sprintf("Delete %q? Your music files stay where they are.", name), func() tea.Cmd {
				if err := m.d.Playlists.Delete(name); err != nil {
					return m.fail(err)
				}
				m.reloadPlaylists()
				if m.playlist == name {
					m.open(sideItem{sec: secSongs})
				}
				m.side = min(m.side, len(m.sideItems())-1)
				return m.showFlash("Deleted "+name, true)
			})
		}
	case "X":
		if name := m.currentPlaylist(); name != "" {
			def := "~/Music"
			m.modal = newPrompt(m, "Export "+name+" to (folder or .m3u8 file)", def, func(dest string) tea.Cmd {
				out, err := m.d.Playlists.Export(name, paths.Expand(dest))
				if err != nil {
					return m.fail(err)
				}
				return m.showFlash("Exported to "+out+" — import it in iTunes / Music", true)
			})
		}
	}
	return nil
}

func (m *Model) rebuildIf(s section) {
	if m.sec == s {
		m.rebuild()
	}
}

func describe(ts []library.Track) string {
	if len(ts) == 1 {
		return ts[0].Title
	}
	return fmt.Sprintf("%d tracks", len(ts))
}

// currentPlaylist is the playlist the user is pointing at: the open one, or
// the one under the sidebar cursor.
func (m *Model) currentPlaylist() string {
	if m.focus == focusSidebar && m.layout() == layoutFull {
		items := m.sideItems()
		if m.side < len(items) && items[m.side].sec == secPlaylist {
			return items[m.side].playlist
		}
		return ""
	}
	if m.sec == secPlaylist {
		return m.playlist
	}
	return ""
}

func (m *Model) enter() tea.Cmd {
	if m.focus == focusSidebar && m.layout() == layoutFull {
		items := m.sideItems()
		if m.side >= len(items) {
			return nil
		}
		it := items[m.side]
		if it.create {
			m.modal = newPrompt(m, "New playlist", "", func(name string) tea.Cmd { return m.createPlaylist(name, nil) })
			return nil
		}
		m.open(it)
		m.focus = focusList
		return nil
	}
	if m.cursor >= len(m.rows) {
		return nil
	}
	r := m.rows[m.cursor]
	if r.track == nil { // an album or artist: open it
		rr := r
		m.drill, m.cursor = &rr, 0
		m.scroll = tui.Scroll{}
		m.filter.SetValue("")
		m.rebuild()
		return nil
	}
	if m.sec == secQueue && m.drill == nil {
		// jump within the queue rather than replacing it
		q := m.pl.State().Queue
		return m.fail(m.pl.PlayList(q, m.cursor))
	}
	return m.playRows(m.cursor)
}

func (m *Model) filterKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.filter.SetValue("")
		m.filtering = false
		m.filter.Blur()
		m.rebuild()
		return nil
	case "enter":
		m.filtering = false
		m.filter.Blur()
		return nil
	case "up", "down":
		m.filtering = false
		m.filter.Blur()
		return m.key(k)
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	m.cursor = 0
	m.rebuild()
	return cmd
}

// ─── Playlists & sources ────────────────────────────────────────────────────

func entriesFor(ts []library.Track) []playlist.Entry {
	out := make([]playlist.Entry, len(ts))
	for i, t := range ts {
		title := t.Title
		if t.Artist != "" {
			title = t.Artist + " - " + t.Title
		}
		out[i] = playlist.Entry{Path: t.Path, Title: title, Duration: t.Duration}
	}
	return out
}

func (m *Model) createPlaylist(name string, ts []library.Track) tea.Cmd {
	if _, err := m.d.Playlists.Create(name); err != nil {
		return m.fail(err)
	}
	if len(ts) > 0 {
		m.d.Playlists.Add(name, entriesFor(ts)...)
		for _, t := range ts {
			m.event("playlist-add", name, t)
		}
	}
	m.reloadPlaylists()
	return m.showFlash(fmt.Sprintf("Created %s", name), true)
}

func (m *Model) addToPlaylist(name string, ts []library.Track) tea.Cmd {
	n, err := m.d.Playlists.Add(name, entriesFor(ts)...)
	if err != nil {
		return m.fail(err)
	}
	if n > 0 {
		for _, t := range ts {
			m.event("playlist-add", name, t)
		}
	}
	m.reloadPlaylists()
	m.rebuildIf(secPlaylist)
	if n == 0 {
		return m.showFlash("Already in "+name, true)
	}
	return m.showFlash(fmt.Sprintf("Added %s to %s", describe(ts), name), true)
}

func (m *Model) removeFromPlaylist() tea.Cmd {
	if m.sec != secPlaylist || m.drill != nil || m.filter.Value() != "" || m.cursor >= len(m.rows) {
		return nil
	}
	if err := m.d.Playlists.Remove(m.playlist, m.cursor); err != nil {
		return m.fail(err)
	}
	m.reloadPlaylists()
	m.rebuild()
	return nil
}

func (m *Model) moveInPlaylist(d int) tea.Cmd {
	if m.sec != secPlaylist || m.drill != nil || m.filter.Value() != "" {
		return nil
	}
	to := m.cursor + d
	if to < 0 || to >= len(m.rows) {
		return nil
	}
	if err := m.d.Playlists.Move(m.playlist, m.cursor, to); err != nil {
		return m.fail(err)
	}
	m.cursor = to
	m.rebuild()
	return nil
}

// importSource adds a folder or file to the library sources and scans.
func (m *Model) importSource(p string) tea.Cmd {
	p = paths.Expand(strings.TrimSpace(p))
	abs, err := filepath.Abs(p)
	if err != nil {
		return m.fail(err)
	}
	if _, err := os.Stat(abs); err != nil {
		return m.fail(err)
	}
	if strings.EqualFold(filepath.Ext(abs), ".m3u8") || strings.EqualFold(filepath.Ext(abs), ".m3u") {
		pl, err := m.d.Playlists.Import(abs)
		if err != nil {
			return m.fail(err)
		}
		m.reloadPlaylists()
		return m.showFlash("Imported playlist "+pl.Name, true)
	}
	if !slices.Contains(m.cfg.Sources, abs) {
		m.cfg.Sources = append(m.cfg.Sources, abs)
		if err := home.Save(m.cfg); err != nil {
			return m.fail(err)
		}
	}
	return m.rescan()
}

// repeatGlyph shows the repeat mode.
func repeatGlyph(r player.Repeat) string {
	switch r {
	case player.RepeatAll:
		return "↻"
	case player.RepeatOne:
		return "↻¹"
	}
	return "↻"
}

// artProbe asks the terminal whether it can show images, if kyro doesn't
// know yet (needed to preview the pixel style).
func (m *Model) artProbe() tea.Cmd {
	if m.px != nil {
		return nil
	}
	m.osc.Expect(2)
	return tea.Raw(art.KittyQuery + art.CellSizeQuery)
}

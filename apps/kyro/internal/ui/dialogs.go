package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/kyro/internal/art"
	"github.com/karim-w/slop/apps/kyro/internal/home"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/apps/kyro/internal/sound"
	"github.com/karim-w/slop/pkg/domain/theme"
	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/tui/termsync"
	"github.com/karim-w/slop/pkg/tui/themed"
)

// modal is a dialog over the player. Update returns nil to close it.
type modal interface {
	Update(m *Model, msg tea.Msg) (modal, tea.Cmd)
	View(m *Model) tui.View
}

// ─── Prompt ─────────────────────────────────────────────────────────────────

type prompt struct {
	title    string
	input    textinput.Model
	onSubmit func(string) tea.Cmd
}

func newPrompt(m *Model, title, initial string, onSubmit func(string) tea.Cmd) *prompt {
	in := themed.Input(m.pal, "› ", 48)
	in.SetValue(initial)
	in.CursorEnd()
	in.Focus()
	return &prompt{title: title, input: in, onSubmit: onSubmit}
}

func (p *prompt) Update(m *Model, msg tea.Msg) (modal, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			return nil, nil
		case "enter":
			v := strings.TrimSpace(p.input.Value())
			if v == "" {
				return nil, nil
			}
			return nil, p.onSubmit(v)
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return p, cmd
}

func (p *prompt) View(m *Model) tui.View {
	return tui.Dialog(p.title, tui.Raw(p.input.View())).Width(min(64, m.w-2)).
		Hints(tui.H("enter", "ok"), tui.H("esc", "cancel"))
}

// ─── Confirm ────────────────────────────────────────────────────────────────

type confirm struct {
	title, message string
	onYes          func() tea.Cmd
}

func newConfirm(title, message string, onYes func() tea.Cmd) *confirm {
	return &confirm{title, message, onYes}
}

func (c *confirm) Update(m *Model, msg tea.Msg) (modal, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "y", "Y", "enter":
			return nil, c.onYes()
		case "n", "N", "esc", "q":
			return nil, nil
		}
	}
	return c, nil
}

func (c *confirm) View(m *Model) tui.View {
	return tui.Dialog(c.title, tui.Text(c.message).Wrap()).Width(min(60, m.w-2)).Danger(true).
		Hints(tui.H("y", "yes"), tui.H("n", "no"))
}

// ─── Add to playlist ────────────────────────────────────────────────────────

type playlistPicker struct {
	tracks []library.Track
	sel    int
	scroll tui.Scroll
}

func newPlaylistPicker(m *Model, ts []library.Track) *playlistPicker {
	return &playlistPicker{tracks: ts}
}

func (p *playlistPicker) Update(m *Model, msg tea.Msg) (modal, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}
	n := len(m.playlists) + 1 // last row: new playlist
	switch k.String() {
	case "esc", "q":
		return nil, nil
	case "up", "k":
		p.sel = (p.sel - 1 + n) % n
	case "down", "j":
		p.sel = (p.sel + 1) % n
	case "enter":
		if p.sel == len(m.playlists) {
			ts := p.tracks
			return newPrompt(m, "New playlist with "+describe(ts), "", func(name string) tea.Cmd {
				return m.createPlaylist(name, ts)
			}), nil
		}
		return nil, m.addToPlaylist(m.playlists[p.sel].Name, p.tracks)
	}
	return p, nil
}

func (p *playlistPicker) View(m *Model) tui.View {
	n := len(m.playlists) + 1
	list := tui.List(n, p.sel, &p.scroll, func(i int, sel bool) tui.View {
		bar := tui.Text("  ")
		if sel {
			bar = tui.Text("▌ ").Fg(tui.Accent2)
		}
		if i == len(m.playlists) {
			return tui.HStack(bar, tui.Text("+ New playlist…").Fg(tui.Accent))
		}
		pl := m.playlists[i]
		return tui.HStack(bar, tui.Text("♫ ").Fg(tui.Accent), tui.Text(pl.Name).Grow(), tui.Textf("%d ", len(pl.Entries)).Fg(tui.Muted))
	}).Highlight(tui.Selection)
	return tui.Dialog("Add "+describe(p.tracks)+" to…", tui.Height(min(n, 12), list)).Width(min(56, m.w-2)).
		Hints(tui.H("⏎", "add"), tui.H("esc", "cancel"))
}

// ─── Themes ─────────────────────────────────────────────────────────────────

// The first two entries are special: the cover's colours (dynamic_theme)
// and the terminal's.
const (
	pickCover = iota
	pickTerminal
)

type themePicker struct {
	origBase *tui.Palette
	origTerm bool
	origDyn  bool
	items    []theme.Theme
	sel      int
	scroll   tui.Scroll
}

func newThemePicker(m *Model) *themePicker {
	tp := &themePicker{origBase: m.base, origTerm: m.termMode, origDyn: m.cfg.DynamicTheme}
	tp.items = append([]theme.Theme{{Name: "cover"}, {Name: "terminal"}}, m.themes...)
	for i, t := range tp.items {
		if i > pickTerminal && t.Name == m.base.T.Name {
			tp.sel = i
		}
	}
	switch {
	case m.cfg.DynamicTheme:
		tp.sel = pickCover
	case m.termMode:
		tp.sel = pickTerminal
	}
	return tp
}

// apply previews the selected entry at once.
func (t *themePicker) apply(m *Model) tea.Cmd {
	switch t.sel {
	case pickCover:
		m.cfg.DynamicTheme = true
		m.termMode, m.base = t.origTerm, t.origBase
	case pickTerminal:
		m.cfg.DynamicTheme, m.termMode = false, true
		m.base = tui.NewPalette(theme.FromTerminal(termsync.ThemeName(), m.term))
	default:
		m.cfg.DynamicTheme, m.termMode = false, false
		m.base = tui.NewPalette(t.items[t.sel])
	}
	return m.retheme(true)
}

func (t *themePicker) Update(m *Model, msg tea.Msg) (modal, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return t, nil
	}
	switch k.String() {
	case "esc", "q":
		m.termMode, m.base, m.cfg.DynamicTheme = t.origTerm, t.origBase, t.origDyn
		return nil, m.retheme(true)
	case "up", "k":
		t.sel = (t.sel - 1 + len(t.items)) % len(t.items)
		return t, t.apply(m)
	case "down", "j":
		t.sel = (t.sel + 1) % len(t.items)
		return t, t.apply(m)
	case "enter":
		name := t.items[t.sel].Name
		if t.sel != pickCover {
			m.cfg.Theme = name
		}
		if err := home.Save(m.cfg); err != nil {
			return nil, m.fail(err)
		}
		var cmd tea.Cmd
		if t.sel == pickTerminal {
			cmd = m.osc.Query()
		}
		if t.sel == pickCover {
			return nil, m.showFlash("Theme · from the cover (c toggles)", true)
		}
		return nil, tea.Batch(cmd, m.showFlash("Theme · "+name, true))
	}
	return t, nil
}

func (t *themePicker) View(m *Model) tui.View {
	list := tui.List(len(t.items), t.sel, &t.scroll, func(i int, sel bool) tui.View {
		th := t.items[i]
		bar := tui.Text("  ")
		if sel {
			bar = tui.Text("▌ ").Fg(tui.Accent2)
		}
		switch i {
		case pickCover:
			return tui.HStack(bar, tui.Text("cover").Bold(), tui.Text("  colours from the playing song's art").Fg(tui.Muted))
		case pickTerminal:
			return tui.HStack(bar, tui.Text("terminal").Bold(), tui.Text("  follows your terminal — Rex included").Fg(tui.Muted))
		}
		var sw []tui.Span
		for _, c := range []string{th.Bg, th.Accent, th.Accent2, th.Accent3, th.Success, th.Warning} {
			sw = append(sw, tui.S("██", tui.Fg(tui.Hex(c))))
		}
		return tui.HStack(bar, tui.Width(20, tui.Text(th.Name)), tui.Line(sw...))
	}).Highlight(tui.Selection)
	return tui.Dialog("Themes", tui.Height(min(len(t.items), max(m.h-10, 4)), list), tui.Text(""),
		tui.Text("custom themes: "+home.ThemesDir()+"/*.json").Fg(tui.Muted)).
		Width(min(60, m.w-2)).Hints(tui.H("↑↓", "preview"), tui.H("⏎", "keep"), tui.H("esc", "revert"))
}

// ─── Help ───────────────────────────────────────────────────────────────────

type helpDialog struct{}

var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"Playback", [][2]string{
		{"space", "play / pause"}, {"] [", "next / previous"}, {"← →", "seek 5s"}, {"⇧← ⇧→", "seek 30s"},
		{"+ -", "volume"}, {"s", "shuffle"}, {"r", "repeat off/all/one"}, {"z", "zen mode"},
	}},
	{"Library", [][2]string{
		{"↑ ↓ / j k", "move"}, {"⏎", "play / open"}, {"esc", "back"}, {"tab", "sidebar / section"},
		{"1-5", "songs … queue, loved"}, {"/", "filter"}, {"e / E", "queue / play next"},
		{"i", "import folder or .m3u8"}, {"ctrl+r", "rescan"},
	}},
	{"Playlists", [][2]string{
		{"a / A", "add track / whole list"}, {"N", "new playlist"}, {"x", "remove track"},
		{"J K", "move track"}, {"R", "rename"}, {"D", "delete"}, {"X", "export .m3u8"},
	}},
	{"Love & stars", [][2]string{
		{"l", "love / unlove"}, {". ,", "more / fewer stars"}, {"0-5", "stars (zen mode)"},
		{"a", "add to playlist (zen too)"},
	}},
	{"Other", [][2]string{{"5", "loved songs"}, {"T", "themes"}, {"c", "theme from the cover on/off"}, {"m", "sound: pure, punch, wide, deep"}, {"o", "play on: this Mac, a TV, a phone"}, {"w", "web player and pairing"}, {"C", "cover art style"}, {"?", "this help"}, {"q", "quit"}}},
}

func (helpDialog) Update(m *Model, msg tea.Msg) (modal, tea.Cmd) {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		return nil, nil
	}
	return helpDialog{}, nil
}

func (helpDialog) View(m *Model) tui.View {
	col := func(i int) tui.View {
		s := helpSections[i]
		return tui.VStack(
			tui.Text(s.title).Fg(tui.Accent).Bold(),
			tui.ForEach(s.keys, func(_ int, k [2]string) tui.View {
				return tui.HStack(tui.Width(11, tui.Text(k[0]).Fg(tui.Accent2).Bold()), tui.Text(k[1]))
			}),
		)
	}
	wide := m.w >= 84
	var body tui.View
	if wide {
		body = tui.VStack(
			tui.HStack(tui.Width(38, col(0)), col(1)),
			tui.Text(""),
			tui.HStack(tui.Width(38, col(2)), col(3)),
			tui.Text(""),
			tui.HStack(tui.Width(38, col(4))),
		)
	} else {
		body = tui.VStack(col(0), tui.Text(""), col(3))
	}
	return tui.Dialog("Keys", body, tui.Text(""),
		tui.Text(fmt.Sprintf("hooks: %s · control: kyro ctl", home.HooksDir())).Fg(tui.Muted)).
		Width(min(84, m.w-2))
}

// ─── Cover art style ────────────────────────────────────────────────────────

type artPicker struct {
	orig   string
	sel    int
	scroll tui.Scroll
}

func newArtPicker(m *Model) *artPicker {
	p := &artPicker{orig: m.cfg.Art}
	for i, md := range art.Modes {
		if string(md) == m.cfg.Art || md == art.Auto && m.cfg.Art == string(art.PixelsMode) {
			p.sel = i
		}
	}
	return p
}

func (p *artPicker) Update(m *Model, msg tea.Msg) (modal, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}
	switch k.String() {
	case "esc", "q":
		m.cfg.Art = p.orig
		return nil, nil
	case "up", "k":
		p.sel = (p.sel - 1 + len(art.Modes)) % len(art.Modes)
		m.cfg.Art = string(art.Modes[p.sel]) // preview live
	case "down", "j", "C":
		p.sel = (p.sel + 1) % len(art.Modes)
		m.cfg.Art = string(art.Modes[p.sel])
	case "enter":
		m.cfg.Art = string(art.Modes[p.sel])
		if err := home.Save(m.cfg); err != nil {
			return nil, m.fail(err)
		}
		return nil, m.showFlash("Cover art: "+artLabel(m, art.Modes[p.sel]), true)
	}
	return p, nil
}

func artLabel(m *Model, md art.Mode) string {
	if md == art.Auto {
		if m.px != nil {
			return "pixels"
		}
		return "pixels (auto)"
	}
	return string(md)
}

func (p *artPicker) View(m *Model) tui.View {
	list := tui.List(len(art.Modes), p.sel, &p.scroll, func(i int, sel bool) tui.View {
		md := art.Modes[i]
		bar := tui.Text("  ")
		if sel {
			bar = tui.Text("▌ ").Fg(tui.Accent2)
		}
		desc := art.Describe(md)
		if md == art.Auto {
			if m.px != nil {
				desc = "the real image: this terminal supports it"
			} else {
				desc = "the real image where supported; blocks here"
			}
		}
		return tui.HStack(bar, tui.Width(15, tui.Text(artLabel(m, md)).Bold()), tui.Text(desc).Fg(tui.Muted))
	}).Highlight(tui.Selection)
	return tui.Dialog("Cover art", tui.Height(len(art.Modes), list), tui.Text(""),
		tui.Text("the cover behind this box previews each style").Fg(tui.Muted)).
		Width(min(74, m.w-2)).Hints(tui.H("↑↓", "preview"), tui.H("⏎", "keep"), tui.H("esc", "revert"))
}

// ─── Sound ──────────────────────────────────────────────────────────────────

// soundPicker chooses the sound mode. Moving the cursor switches the sound
// right away, so modes can be compared on the song that's playing.
type soundPicker struct {
	orig   string
	sel    int
	scroll tui.Scroll
}

func newSoundPicker(m *Model) *soundPicker {
	p := &soundPicker{orig: sound.Find(m.cfg.Audio.Sound).Name}
	for i, md := range sound.Modes {
		if md.Name == p.orig {
			p.sel = i
		}
	}
	return p
}

func (p *soundPicker) Update(m *Model, msg tea.Msg) (modal, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}
	n := len(sound.Modes)
	switch k.String() {
	case "esc", "q":
		return nil, m.setSound(p.orig)
	case "up", "k":
		p.sel = (p.sel - 1 + n) % n
		return p, m.setSound(sound.Modes[p.sel].Name)
	case "down", "j", "m":
		p.sel = (p.sel + 1) % n
		return p, m.setSound(sound.Modes[p.sel].Name)
	case "enter":
		name := sound.Modes[p.sel].Name
		if cmd := m.setSound(name); cmd != nil {
			return nil, cmd
		}
		if err := home.Save(m.cfg); err != nil {
			return nil, m.fail(err)
		}
		return nil, m.showFlash("Sound · "+name, true)
	}
	return p, nil
}

func (p *soundPicker) View(m *Model) tui.View {
	list := tui.List(len(sound.Modes), p.sel, &p.scroll, func(i int, sel bool) tui.View {
		md := sound.Modes[i]
		bar := tui.Text("  ")
		if sel {
			bar = tui.Text("▌ ").Fg(tui.Accent2)
		}
		return tui.HStack(bar, tui.Width(9, tui.Text(md.Name).Bold()), tui.Text(md.Desc).Fg(tui.Muted))
	}).Highlight(tui.Selection)
	note := "plays live: move to compare on this song"
	if !m.pl.State().Active {
		note = "start a song to hear each mode as you move"
	}
	return tui.Dialog("Sound", tui.Height(len(sound.Modes), list), tui.Text(""),
		tui.Text(note).Fg(tui.Muted),
		tui.Text("every mode keeps peaks under full scale").Fg(tui.Muted)).
		Width(min(76, m.w-2)).Hints(tui.H("↑↓", "listen"), tui.H("⏎", "keep"), tui.H("esc", "revert"))
}

// Package ui is kyro's terminal interface. It adapts to the space it gets:
// a full library browser in a big window, a now-playing card with a list in
// a tiling pane, a single line in a sliver, and a zen view with large cover
// art on demand.
package ui

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/karim-w/slop/apps/kyro/internal/applemusic"
	"github.com/karim-w/slop/apps/kyro/internal/art"
	"github.com/karim-w/slop/apps/kyro/internal/engine"
	"github.com/karim-w/slop/apps/kyro/internal/home"
	"github.com/karim-w/slop/apps/kyro/internal/hooks"
	"github.com/karim-w/slop/apps/kyro/internal/ipc"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/apps/kyro/internal/player"
	"github.com/karim-w/slop/apps/kyro/internal/playlist"
	"github.com/karim-w/slop/apps/kyro/internal/ratings"
	"github.com/karim-w/slop/apps/kyro/internal/sound"
	"github.com/karim-w/slop/pkg/domain/theme"
	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/tui/termsync"
	"github.com/karim-w/slop/pkg/tui/themed"
)

// Deps are what the UI works with. Engine may be nil (mpv missing): the
// library still works and the error is shown.
type Deps struct {
	Config    home.Config
	Library   *library.Library
	Playlists playlist.Store
	Engine    engine.Engine
	EngineErr error
	Hooks     *hooks.Dispatcher
	Ratings   *ratings.Store // loves and stars; nil disables them
	Log       *slog.Logger
	ThemeName string // --theme flag, overrides config
}

// section is what the main list shows.
type section int

const (
	secSongs section = iota
	secAlbums
	secArtists
	secQueue
	secLoved
	secPlaylist
)

var sectionNames = map[section]string{secSongs: "Songs", secAlbums: "Albums", secArtists: "Artists", secQueue: "Queue", secLoved: "Loved", secPlaylist: "Playlist"}

type focus int

const (
	focusList focus = iota
	focusSidebar
)

// row is one line of the main list: a track, or an album / artist group.
type row struct {
	track  *library.Track
	group  string // album or artist name
	sub    string // artist of an album
	count  int
	tracks []library.Track
}

// Model is the Bubble Tea model.
type Model struct {
	d      Deps
	cfg    home.Config
	lib    *library.Library
	pl     *player.Player
	art    *art.Cache
	tracks []library.Track // whole library, sorted

	// navigation
	sec       section
	playlist  string // open playlist (secPlaylist)
	drill     *row   // open album / artist inside its section
	rows      []row
	cursor    int
	scroll    tui.Scroll
	focus     focus
	side      int // sidebar cursor
	sideScr   tui.Scroll
	playlists []playlist.Playlist
	filter    textinput.Model
	filtering bool

	zen     bool
	modal   modal
	flash   string
	flashOK bool
	flashID int
	scan    string // scan progress, "" when idle
	tick    int    // animation frame

	// theme
	pal      *tui.Palette // what is drawn now
	base     *tui.Palette // the theme picked with T (or the terminal's)
	themes   []theme.Theme
	termMode bool
	term     theme.TermColors
	osc      termsync.Gate

	// Dynamic theme: palettes derived from covers, by art key (nil = the
	// cover has no art), and the fade between one palette and the next.
	covers      map[string]*theme.Theme
	coverBusy   string // art key being derived
	themeTarget string // what the palette is heading to
	coverOn     bool   // the palette comes from a cover (or is fading)
	fade        fade

	// Cover art as real pixels, when the terminal speaks the kitty graphics
	// protocol (detected at start); nil means block characters.
	px *art.Pixels

	notify       chan func() // hook events, delivered in order off the UI goroutine
	pending      []tea.Cmd   // commands queued outside Update (IPC), run on the next one
	w, h         int
	cellW, cellH int // cell size in pixels, if the terminal said
}

// New builds the model.
func New(d Deps) *Model {
	m := &Model{d: d, cfg: d.Config, lib: d.Library, art: art.NewCache(48), notify: make(chan func(), 64), covers: map[string]*theme.Theme{}}
	go func() {
		for f := range m.notify {
			f()
		}
	}()
	eng := d.Engine
	if eng == nil {
		eng = nullEngine{}
	}
	m.pl = player.New(eng, d.Config.Volume, d.Config.Shuffle, player.Repeat(d.Config.Repeat))
	m.themes = theme.All(home.ThemesDir())
	name := d.ThemeName
	if name == "" {
		name = d.Config.Theme
	}
	if name == "terminal" || name == "rex" || name == "" && termsync.InRex() {
		m.termMode = true
		m.setBase(tui.NewPalette(theme.FromTerminal(termsync.ThemeName(), m.term)))
	} else {
		t, _ := theme.Find(m.themes, name)
		m.setBase(tui.NewPalette(t))
	}
	m.reloadLibrary()
	m.reloadPlaylists()
	m.rebuild()
	if d.EngineErr != nil {
		m.flash, m.flashOK = d.EngineErr.Error(), false
	}
	return m
}

// setBase sets the theme used without a cover. While a cover theme shows,
// it takes effect when the cover theme goes.
func (m *Model) setBase(p *tui.Palette) {
	m.base = p
	if !m.coverOn || m.pal == nil {
		m.setPalette(p)
		m.themeTarget = baseTarget(p)
	}
}

func (m *Model) setPalette(p *tui.Palette) {
	m.pal = p
	v := m.filter.Value()
	m.filter = themed.Input(p, "/ ", 30)
	m.filter.SetValue(v)
	if m.filtering {
		m.filter.Focus()
	}
}

// ─── Messages ───────────────────────────────────────────────────────────────

type engineMsg engine.Event

// IPCMsg carries a control-socket request into the UI goroutine.
type IPCMsg struct {
	Req   ipc.Request
	Reply chan ipc.Response
}

type scanMsg struct {
	seen int
	done bool
	res  library.ScanResult
}

type flashMsg struct {
	text string
	ok   bool
}

type clearFlashMsg struct{ id int }

type tickMsg struct{}

// WaitEngine re-arms reading one engine event.
func WaitEngine(e engine.Engine) tea.Cmd {
	if e == nil {
		return nil
	}
	return func() tea.Msg { return engineMsg(<-e.Events()) }
}

func animTick() tea.Cmd {
	return tea.Tick(time.Second/8, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{WaitEngine(m.d.Engine), animTick()}
	if m.termMode {
		cmds = append(cmds, m.osc.Query())
	}
	if len(m.cfg.Sources) > 0 {
		cmds = append(cmds, m.rescan())
	}
	if m.d.Ratings != nil && m.cfg.AppleMusic != "off" && applemusic.Available() {
		cmds = append(cmds, m.syncPending(false))
	}
	if mode := art.Mode(m.cfg.Art); mode == art.Auto || mode == art.PixelsMode {
		// Ask whether the terminal can show images, and its cell size.
		m.osc.Expect(2)
		cmds = append(cmds, tea.Raw(art.KittyQuery+art.CellSizeQuery))
	}
	return tea.Batch(cmds...)
}

// ─── Update ─────────────────────────────────────────────────────────────────

// Update handles a message, then sends any cover images the last frame
// asked for (images are queued while drawing, which can't send them).
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	if len(m.pending) > 0 {
		cmd = tea.Batch(append([]tea.Cmd{cmd}, m.pending...)...)
		m.pending = nil
	}
	if c := m.retheme(false); c != nil {
		cmd = tea.Batch(cmd, c)
	}
	if m.px != nil && m.px.Pending() {
		cmd = tea.Batch(cmd, tea.Raw(m.px.Flush()))
	}
	return model, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case tea.BackgroundColorMsg:
		return m, m.onTermColor(func() { m.term.Bg = msg.Color })
	case tea.ForegroundColorMsg:
		return m, m.onTermColor(func() { m.term.Fg = msg.Color })
	case uv.UnknownOscEvent:
		if i, c, ok := termsync.ParseOSC4(string(msg)); ok {
			return m, m.onTermColor(func() { m.term.ANSI[i] = c })
		}
		return m, nil
	case uv.KittyGraphicsEvent:
		if msg.Options.ID != art.KittyQueryID {
			return m, nil
		}
		if string(msg.Payload) == "OK" && m.px == nil {
			m.px = art.NewPixels()
			m.px.SetCellSize(m.cellW, m.cellH)
		}
		return m, m.osc.Reply()
	case uv.CellSizeEvent:
		m.cellW, m.cellH = msg.Width, msg.Height
		if m.px != nil {
			m.px.SetCellSize(msg.Width, msg.Height)
		}
		return m, m.osc.Reply()
	case termsync.FlushMsg:
		return m, m.osc.Flush()
	case coverThemeMsg:
		if len(m.covers) > 64 {
			clear(m.covers)
		}
		m.covers[msg.key] = msg.theme
		if m.coverBusy == msg.key {
			m.coverBusy = ""
		}
		return m, nil
	case fadeMsg:
		return m, m.onFade(msg)
	case tea.FocusMsg:
		// While a cover theme shows, kyro has set the terminal's background
		// itself, so asking for it would only echo the cover back.
		if m.termMode && !m.coverOn {
			return m, m.osc.Query()
		}
		return m, nil

	case engineMsg:
		return m, tea.Batch(m.onEngine(engine.Event(msg)), WaitEngine(m.d.Engine))

	case IPCMsg:
		msg.Reply <- m.onIPC(msg.Req)
		return m, nil

	case scanMsg:
		if !msg.done {
			m.scan = fmt.Sprintf("scanning… %d files", msg.seen)
			return m, nil
		}
		m.scan = ""
		m.reloadLibrary()
		m.rebuild()
		r := msg.res
		text := fmt.Sprintf("Library: %d tracks", r.Total)
		if r.Added > 0 {
			text += fmt.Sprintf(" · %d new", r.Added)
		}
		if r.Missing > 0 {
			text += fmt.Sprintf(" · %d missing", r.Missing)
		}
		return m, m.showFlash(text, true)

	case flashMsg:
		return m, m.showFlash(msg.text, msg.ok)
	case syncDoneMsg:
		if msg.err != nil {
			return m, m.showFlash("Apple Music: "+msg.err.Error(), false)
		}
		return m, nil
	case clearFlashMsg:
		if msg.id == m.flashID {
			m.flash = ""
		}
		return m, nil

	case tickMsg:
		m.tick++
		return m, animTick()

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, m.quit()
		}
		if m.modal != nil {
			next, cmd := m.modal.Update(m, msg)
			m.modal = next
			return m, cmd
		}
		if m.filtering {
			return m, m.filterKey(msg)
		}
		return m, m.key(msg)

	case tea.MouseWheelMsg:
		if m.modal == nil {
			if msg.Button == tea.MouseWheelUp {
				m.move(-3)
			} else {
				m.move(3)
			}
		}
		return m, nil
	}
	if m.modal != nil {
		next, cmd := m.modal.Update(m, msg)
		m.modal = next
		return m, cmd
	}
	return m, nil
}

func (m *Model) onTermColor(set func()) tea.Cmd {
	set()
	if m.termMode && m.term.Complete() {
		m.setBase(tui.NewPalette(theme.FromTerminal(termsync.ThemeName(), m.term)))
	}
	return m.osc.Reply()
}

func (m *Model) showFlash(text string, ok bool) tea.Cmd {
	m.flashID++
	id := m.flashID
	m.flash, m.flashOK = text, ok
	d := 4 * time.Second
	if !ok {
		d = 8 * time.Second
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return clearFlashMsg{id} })
}

func (m *Model) fail(err error) tea.Cmd {
	if err == nil {
		return nil
	}
	return m.showFlash(err.Error(), false)
}

// ─── Playback ───────────────────────────────────────────────────────────────

func (m *Model) onEngine(e engine.Event) tea.Cmd {
	if e.Kind == engine.Remote {
		return m.fail(m.remote(e.Command))
	}
	change, err := m.pl.Handle(e)
	var cmds []tea.Cmd
	if err != nil {
		cmds = append(cmds, m.fail(err))
	}
	// Learn durations the headers didn't give us.
	if e.Kind == engine.Duration {
		if t, ok := m.lib.Get(m.pl.State().Track.ID); ok && t.Duration == 0 {
			t.Duration = e.Duration
			m.lib.Update(t)
		}
	}
	if change != "" {
		m.announce(change)
	}
	if e.Kind == engine.Exited && e.Err != nil {
		m.d.Engine = nil
	}
	return tea.Batch(cmds...)
}

// announce tells hooks, the now-playing file and socket subscribers.
func (m *Model) announce(c player.Change) {
	if m.d.Hooks == nil {
		return
	}
	ev := m.snapshot(string(c))
	t := m.pl.State().Track
	active := m.pl.State().Active
	select {
	case m.notify <- func() {
		if active && ev.Track != nil {
			ev.Track.Art = art.Export(t, home.ArtCacheDir())
		}
		m.d.Hooks.Fire(ev)
	}:
	default: // hooks are far behind; skip rather than block the UI
	}
}

// snapshot is the current state in the shape hooks and the socket use.
func (m *Model) snapshot(event string) hooks.Event {
	s := m.pl.State()
	ev := hooks.Event{Event: event, Playing: s.Active && !s.Paused, Position: s.Pos.Seconds(),
		Volume: s.Volume, Shuffle: s.Shuffle, Repeat: string(s.Repeat)}
	if s.Active {
		t := s.Track
		if s.Duration > 0 {
			t.Duration = s.Duration
		}
		ev.Track = hooks.FromTrack(t, "")
		r := m.d.Ratings.Get(t.Path)
		ev.Track.Loved, ev.Track.Stars = r.Loved, r.Stars
	}
	return ev
}

func (m *Model) playRows(start int) tea.Cmd {
	var ts []library.Track
	idx := 0
	for i, r := range m.rows {
		if r.track == nil {
			continue
		}
		if i == start {
			idx = len(ts)
		}
		ts = append(ts, *r.track)
	}
	if len(ts) == 0 {
		return nil
	}
	return m.fail(m.pl.PlayList(ts, idx))
}

func (m *Model) quit() tea.Cmd {
	s := m.pl.State()
	m.cfg.Volume, m.cfg.Shuffle, m.cfg.Repeat = s.Volume, s.Shuffle, string(s.Repeat)
	_ = home.Save(m.cfg)
	_ = m.lib.Save()
	if s.Active {
		m.d.Hooks.Fire(hooks.Event{Event: "stop", Volume: s.Volume})
	}
	if m.d.Engine != nil {
		m.d.Engine.Close()
	}
	quit := tea.Quit
	if m.px != nil { // free the cover images held by the terminal
		quit = tea.Sequence(tea.Raw(m.px.Cleanup()), tea.Quit)
	}
	return m.osc.WhenQuiet(quit)
}

// ─── Library & sections ─────────────────────────────────────────────────────

func (m *Model) reloadLibrary() { m.tracks = m.lib.All() }

func (m *Model) reloadPlaylists() {
	ps, err := m.d.Playlists.List()
	if err == nil {
		m.playlists = ps
	}
}

// rescan indexes the sources in the background.
func (m *Model) rescan() tea.Cmd {
	m.scan = "scanning…"
	lib, sources := m.lib, slices.Clone(m.cfg.Sources)
	return func() tea.Msg {
		res := lib.Scan(sources, nil)
		lib.Save()
		return scanMsg{done: true, res: res}
	}
}

// rebuild recomputes the rows for the current section and filter.
func (m *Model) rebuild() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	match := func(t library.Track) bool {
		if q == "" {
			return true
		}
		return strings.Contains(strings.ToLower(t.Title+" "+t.Artist+" "+t.Album+" "+t.AlbumArtist+" "+t.Genre), q)
	}
	var rows []row
	add := func(ts []library.Track) {
		for i := range ts {
			if match(ts[i]) {
				t := ts[i]
				rows = append(rows, row{track: &t})
			}
		}
	}
	switch {
	case m.drill != nil:
		add(m.drill.tracks)
	case m.sec == secSongs:
		add(m.tracks)
	case m.sec == secAlbums:
		rows = m.groups(func(t library.Track) (string, string) { return orUnknown(t.Album, "Unknown album"), t.SortArtist() }, q)
	case m.sec == secArtists:
		rows = m.groups(func(t library.Track) (string, string) { return orUnknown(t.SortArtist(), "Unknown artist"), "" }, q)
	case m.sec == secQueue:
		add(m.pl.State().Queue)
	case m.sec == secLoved:
		loved := m.d.Ratings.Loved()
		var ts []library.Track
		for _, t := range m.tracks {
			if loved[t.Path] {
				ts = append(ts, t)
			}
		}
		add(ts)
	case m.sec == secPlaylist:
		add(m.playlistTracks(m.playlist))
	}
	m.rows = rows
	m.cursor = min(m.cursor, max(len(rows)-1, 0))
}

func orUnknown(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func (m *Model) groups(key func(library.Track) (string, string), q string) []row {
	idx := map[string]int{}
	var rows []row
	for _, t := range m.tracks {
		name, sub := key(t)
		k := strings.ToLower(name + "\x00" + sub)
		i, ok := idx[k]
		if !ok {
			i = len(rows)
			idx[k] = i
			rows = append(rows, row{group: name, sub: sub})
		}
		rows[i].tracks = append(rows[i].tracks, t)
		rows[i].count++
	}
	if q != "" {
		rows = slices.DeleteFunc(rows, func(r row) bool {
			return !strings.Contains(strings.ToLower(r.group+" "+r.sub), q)
		})
	}
	slices.SortStableFunc(rows, func(a, b row) int { return strings.Compare(strings.ToLower(a.group), strings.ToLower(b.group)) })
	return rows
}

// playlistTracks resolves a playlist's entries against the library; files
// kyro hasn't indexed are read on the spot.
func (m *Model) playlistTracks(name string) []library.Track {
	p, err := m.d.Playlists.Get(name)
	if err != nil {
		return nil
	}
	out := make([]library.Track, 0, len(p.Entries))
	for _, e := range p.Entries {
		if t, ok := m.lib.ByPath(e.Path); ok {
			out = append(out, t)
			continue
		}
		t, err := library.Read(e.Path)
		if err != nil {
			t = library.Track{ID: library.IDFor(e.Path), Path: e.Path, Title: orUnknown(e.Title, filepath.Base(e.Path)), Missing: true, Duration: e.Duration}
		}
		out = append(out, t)
	}
	return out
}

// selectedTracks is what an action applies to: the track under the cursor,
// or every track of the album / artist under it.
func (m *Model) selectedTracks() []library.Track {
	if m.cursor >= len(m.rows) {
		return nil
	}
	r := m.rows[m.cursor]
	if r.track != nil {
		return []library.Track{*r.track}
	}
	return r.tracks
}

// ─── Control socket ─────────────────────────────────────────────────────────

func (m *Model) onIPC(r ipc.Request) ipc.Response {
	if r.Cmd == "love" || r.Cmd == "rate" {
		defer func() { m.pending = append(m.pending, m.syncCmd()) }()
	}
	var err error
	s := m.pl.State()
	switch r.Cmd {
	case "status":
	case "toggle":
		err = m.pl.Toggle()
	case "play":
		if !s.Active {
			err = m.pl.Toggle()
		} else {
			err = m.pl.SetPaused(false)
		}
	case "pause":
		err = m.pl.SetPaused(true)
	case "next":
		err = m.pl.Next()
	case "prev":
		err = m.pl.Prev()
	case "stop":
		err = m.pl.Stop()
		m.announce(player.Stopped)
	case "love", "rate":
		if !s.Active {
			return ipc.Response{Error: "nothing playing"}
		}
		t := s.Track
		if r.Cmd == "love" {
			m.rate(t, func(x *ratings.Rating) { x.Loved = !x.Loved })
		} else {
			var n int
			if _, e := fmt.Sscanf(r.Arg, "%d", &n); e != nil || n < 0 || n > 5 {
				return ipc.Response{Error: "rate needs 0-5"}
			}
			m.rate(t, func(x *ratings.Rating) { x.Stars = n })
		}
	case "sound": // a mode by name, or the next one
		name := r.Arg
		if name == "" {
			cur := sound.Find(m.cfg.Audio.Sound).Name
			for i, md := range sound.Modes {
				if md.Name == cur {
					name = sound.Modes[(i+1)%len(sound.Modes)].Name
				}
			}
		} else if sound.Find(name).Name != strings.ToLower(name) {
			return ipc.Response{Error: "sound is one of pure, punch, wide, deep"}
		}
		m.pending = append(m.pending, m.setSound(name))
		err = home.Save(m.cfg)
	case "shuffle":
		m.pl.ToggleShuffle()
	case "repeat":
		m.pl.CycleRepeat()
	case "seek":
		var v float64
		if _, e := fmt.Sscanf(strings.TrimPrefix(r.Arg, "+"), "%g", &v); e != nil {
			return ipc.Response{Error: "seek needs seconds, +N or -N"}
		}
		d := time.Duration(v * float64(time.Second))
		if strings.HasPrefix(r.Arg, "+") || strings.HasPrefix(r.Arg, "-") {
			err = m.pl.SeekBy(d)
		} else {
			err = m.pl.Seek(d)
		}
	case "volume":
		var v int
		if _, e := fmt.Sscanf(strings.TrimPrefix(r.Arg, "+"), "%d", &v); e != nil {
			return ipc.Response{Error: "volume needs 0-100, +N or -N"}
		}
		if strings.HasPrefix(r.Arg, "+") || strings.HasPrefix(r.Arg, "-") {
			v += s.Volume
		}
		err = m.pl.SetVolume(v)
	default:
		return ipc.Response{Error: "unknown command " + r.Cmd}
	}
	if err != nil {
		return ipc.Response{Error: err.Error()}
	}
	st := m.snapshot("status")
	return ipc.Response{OK: true, State: &st}
}

// nullEngine stands in when mpv is unavailable.
type nullEngine struct{}

var errNoEngine = fmt.Errorf("can't play: mpv is not available (brew install mpv)")

func (nullEngine) Play(string) error           { return errNoEngine }
func (nullEngine) Next(string) error           { return nil }
func (nullEngine) SetPaused(bool) error        { return errNoEngine }
func (nullEngine) Seek(time.Duration) error    { return nil }
func (nullEngine) SetVolume(int) error         { return nil }
func (nullEngine) SetFilter(string) error      { return nil }
func (nullEngine) Stop() error                 { return nil }
func (nullEngine) Events() <-chan engine.Event { return nil }
func (nullEngine) Close() error                { return nil }

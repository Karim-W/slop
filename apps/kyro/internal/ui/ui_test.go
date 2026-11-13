package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
	"github.com/karim-w/slop/apps/kyro/internal/stats"
	"github.com/karim-w/slop/apps/kyro/internal/web"
)

// fakeEngine plays instantly and reports what a real one would.
type fakeEngine struct{ playing, af string }

func (f *fakeEngine) Play(p string) error         { f.playing = p; return nil }
func (f *fakeEngine) Next(string) error           { return nil }
func (f *fakeEngine) SetPaused(bool) error        { return nil }
func (f *fakeEngine) Seek(time.Duration) error    { return nil }
func (f *fakeEngine) SetVolume(int) error         { return nil }
func (f *fakeEngine) SetFilter(af string) error   { f.af = af; return nil }
func (f *fakeEngine) Stop() error                 { return nil }
func (f *fakeEngine) Events() <-chan engine.Event { return nil }
func (f *fakeEngine) Close() error                { return nil }

func testLibrary(t *testing.T) (*library.Library, string) {
	t.Helper()
	dir := t.TempDir()
	if src := os.Getenv("KYRO_FIXTURES"); src != "" { // real files with cover art
		lib, _ := library.Open(filepath.Join(dir, "lib.json"))
		lib.Scan([]string{src}, nil)
		return lib, dir
	}
	lib, _ := library.Open(filepath.Join(dir, "lib.json"))
	add := func(title, artist, album string, n int, codec string, rate, bits, ch int) {
		p := filepath.Join(dir, artist, album, fmt.Sprintf("%02d %s.flac", n, title))
		lib.Update(library.Track{ID: library.IDFor(p), Path: p, Title: title, Artist: artist, Album: album,
			TrackNo: n, Duration: 3*time.Minute + time.Duration(n)*7*time.Second, Codec: codec,
			SampleRate: rate, BitDepth: bits, Channels: ch, Year: 2023})
	}
	add("Solar Wind", "Nebula Drift", "Afterglow", 1, "FLAC", 96000, 24, 2)
	add("Event Horizon", "Nebula Drift", "Afterglow", 2, "FLAC", 96000, 24, 2)
	add("Surround Bloom", "Nebula Drift", "Afterglow", 3, "FLAC", 48000, 16, 6)
	add("Origami", "Kite Lines", "Paper Skies", 1, "ALAC", 44100, 16, 2)
	add("Updraft", "Kite Lines", "Paper Skies", 2, "MP3", 44100, 0, 2)
	return lib, dir
}

func newTestModel(t *testing.T, w, h int) (*Model, *fakeEngine) {
	t.Helper()
	t.Setenv("KYRO_HOME", t.TempDir())
	home.Ensure()
	lib, _ := testLibrary(t)
	eng := &fakeEngine{}
	cfg := home.Default()
	cfg.Sources = nil
	rs, _ := ratings.Open(home.RatingsPath())
	cfg.AppleMusic = "off" // never drive the real Music app from tests
	m := New(Deps{Config: cfg, Library: lib, Playlists: playlist.Store{Dir: home.PlaylistsDir()},
		Engine: eng, Hooks: &hooks.Dispatcher{}, Ratings: rs, ThemeName: "nebula"})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m, eng
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		default:
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		m.Update(msg)
	}
}

func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// startPlaying plays the selected row and feeds the engine events a real
// backend sends.
func startPlaying(m *Model, eng *fakeEngine) {
	press(m, "enter")
	m.Update(engineMsg(engine.Event{Kind: engine.Started, Path: eng.playing}))
	m.Update(engineMsg(engine.Event{Kind: engine.Params, Source: engine.AudioParams{SampleRate: 96000, Channels: 2, Layout: "stereo"}}))
	m.Update(engineMsg(engine.Event{Kind: engine.Params, Output: engine.AudioParams{SampleRate: 48000, Channels: 2, Layout: "stereo"}}))
	m.Update(engineMsg(engine.Event{Kind: engine.Position, Pos: 83 * time.Second}))
}

func screen(t *testing.T, m *Model) string {
	t.Helper()
	content := m.View().Content
	if dir := os.Getenv("KYRO_SHOTS"); dir != "" {
		os.WriteFile(filepath.Join(dir, strings.ReplaceAll(t.Name(), "/", "_")+".ansi"), []byte(content), 0o644)
	}
	out := ansi.Strip(content)
	lines := strings.Split(out, "\n")
	if len(lines) != m.h {
		t.Fatalf("%d lines, want %d\n%s", len(lines), m.h, out)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.w {
			t.Fatalf("line %d is %d cells, want %d\n%s", i, w, m.w, out)
		}
	}
	return out
}

func must(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Fatalf("missing %q\n%s", w, out)
		}
	}
}

func TestLayouts(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h int
		want []string
	}{
		{"full", 130, 36, []string{"kyro", "LIBRARY", "PLAYLISTS", "Songs", "Origami", "Nebula Drift", "FLAC 24/96", "Now playing", "→ 48 kHz", "space play"}},
		{"compact pane", 62, 22, []string{"Origami", "Kite Lines — Paper Skies", "1:23", "tab ›"}},
		{"small pane", 50, 12, []string{"Origami", "1:23"}},
		{"sliver", 60, 3, []string{"▶ Origami — Kite Lines", "1:23/"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, eng := newTestModel(t, tc.w, tc.h)
			startPlaying(m, eng)
			must(t, screen(t, m), tc.want...)
		})
	}
}

func TestZen(t *testing.T) {
	for _, size := range [][2]int{{120, 34}, {62, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m, eng := newTestModel(t, size[0], size[1])
			startPlaying(m, eng)
			press(m, "z")
			must(t, screen(t, m), "Origami", "Kite Lines — Paper Skies", "◂◂", "❚❚", "▸▸", "1:23", "exit zen")
		})
	}
}

func TestEmptyLibraryInvitesImport(t *testing.T) {
	t.Setenv("KYRO_HOME", t.TempDir())
	home.Ensure()
	lib, _ := library.Open(filepath.Join(t.TempDir(), "l.json"))
	m := New(Deps{Config: home.Default(), Library: lib, Playlists: playlist.Store{Dir: home.PlaylistsDir()}, Engine: &fakeEngine{}, Hooks: &hooks.Dispatcher{}, ThemeName: "nebula"})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	must(t, screen(t, m), "your library is empty", "press i to import")
}

func TestPlaylistsAreM3U8Files(t *testing.T) {
	m, _ := newTestModel(t, 130, 36)
	press(m, "a")
	must(t, screen(t, m), "Add ", "New playlist")
	press(m, "enter") // the only row: new playlist
	typeText(m, "Night Drive")
	press(m, "enter")
	press(m, "down", "a", "enter") // add the second track to it
	p, err := playlist.Store{Dir: home.PlaylistsDir()}.Get("Night Drive")
	if err != nil || len(p.Entries) != 2 {
		t.Fatalf("%v %+v", err, p)
	}
	b, _ := os.ReadFile(p.File)
	if !strings.HasPrefix(string(b), "#EXTM3U\n#PLAYLIST:Night Drive\n#EXTINF:") {
		t.Fatalf("not an extended m3u:\n%s", b)
	}
	// open it from the sidebar
	press(m, "tab")
	for range 5 { // past songs, albums, artists, queue, loved
		press(m, "down")
	}
	press(m, "enter")
	must(t, screen(t, m), "♫ Night Drive", "Origami", "Updraft", "x remove")
	press(m, "x")
	p, _ = playlist.Store{Dir: home.PlaylistsDir()}.Get("Night Drive")
	if len(p.Entries) != 1 {
		t.Fatalf("remove: %d entries", len(p.Entries))
	}
}

func TestAlbumsDrillDownAndFilter(t *testing.T) {
	m, _ := newTestModel(t, 130, 36)
	press(m, "2")
	must(t, screen(t, m), "Afterglow", "Paper Skies", "3 tracks")
	press(m, "enter")
	must(t, screen(t, m), "Albums › Afterglow", "Event Horizon")
	press(m, "esc", "1", "/")
	typeText(m, "origa")
	out := screen(t, m)
	must(t, out, "Origami")
	if strings.Contains(out, "Updraft") {
		t.Fatal("filter kept a non-matching track")
	}
}

func TestDialogsRender(t *testing.T) {
	for _, k := range []string{"?", "T", "N", "i", "o", "w"} {
		m, _ := newTestModel(t, 100, 30)
		press(m, k)
		if m.modal == nil {
			t.Fatalf("%s opened nothing", k)
		}
		screen(t, m)
	}
}

func TestControlSocketCommands(t *testing.T) {
	m, eng := newTestModel(t, 100, 30)
	startPlaying(m, eng)
	r := m.onIPC(ipc.Request{Cmd: "volume", Arg: "-20"})
	if !r.OK || r.State.Volume != 50 || r.State.Track.Title != "Origami" {
		t.Fatalf("%+v", r)
	}
	if r := m.onIPC(ipc.Request{Cmd: "bogus"}); r.OK {
		t.Fatal("unknown command accepted")
	}
}

// TestScreenshots writes frames for docs/screenshots (see
// scripts/screenshots.sh). It needs KYRO_FIXTURES (music with cover art) and
// KYRO_SHOTS (output folder).
func TestScreenshots(t *testing.T) {
	if os.Getenv("KYRO_FIXTURES") == "" || os.Getenv("KYRO_SHOTS") == "" {
		t.Skip("set KYRO_FIXTURES and KYRO_SHOTS")
	}
	for _, tc := range []struct {
		name string
		w, h int
		zen  bool
		art  string
	}{{"full", 132, 36, false, ""}, {"pane", 64, 22, false, ""}, {"zen", 120, 34, true, ""}, {"zen-ascii", 120, 34, true, "ascii"}, {"zen-dynamic", 120, 34, true, "dynamic"}} {
		t.Run(tc.name, func(t *testing.T) {
			m, eng := newTestModel(t, tc.w, tc.h)
			press(m, "/")
			typeText(m, "solar")
			press(m, "enter")
			startPlaying(m, eng)
			press(m, "esc")
			switch tc.art {
			case "":
			case "dynamic": // the theme from the cover
				m.cfg.DynamicTheme = true
				settle(m, m.retheme(false))
				settle(m, m.retheme(false))
				// The terminal background comes from the view, not the cells.
				os.WriteFile(filepath.Join(os.Getenv("KYRO_SHOTS"), "TestScreenshots_zen-dynamic.colors"),
					[]byte(m.pal.T.Bg+" "+m.pal.T.Text+"\n"), 0o644)
			default:
				m.cfg.Art = tc.art
			}
			if tc.zen {
				press(m, "z", "4", "l")
			}
			m.tick = 3
			screen(t, m)
		})
	}
}

func TestCoverStylePickerPreviewsAndSaves(t *testing.T) {
	m, eng := newTestModel(t, 100, 30)
	startPlaying(m, eng)
	press(m, "C")
	must(t, screen(t, m), "Cover art", "sextant", "ascii", "ascii-mono", "plain characters")
	for _, want := range []string{"sextant", "quadrant", "halfblock", "ascii", "ascii-mono"} {
		press(m, "down")
		if m.cfg.Art != want {
			t.Fatalf("preview %s, want %s", m.cfg.Art, want)
		}
		screen(t, m)
	}
	press(m, "esc")
	if m.cfg.Art != "auto" {
		t.Fatalf("esc should revert, got %s", m.cfg.Art)
	}
	press(m, "C", "down", "down", "down", "down", "enter")
	if cfg, _ := home.Load(); cfg.Art != "ascii" {
		t.Fatalf("saved %s", cfg.Art)
	}
}

func TestLoveAndStarsEverywhere(t *testing.T) {
	m, eng := newTestModel(t, 130, 36)
	startPlaying(m, eng) // Origami
	// browsing: keys act on the selected row
	press(m, "down", "l", ".", ".", ".")
	updraft := m.rows[1].track.Path
	if r := m.d.Ratings.Get(updraft); !r.Loved || r.Stars != 3 {
		t.Fatalf("selected row: %+v", r)
	}
	must(t, screen(t, m), "♥ ★★★☆☆")
	press(m, ",")
	if m.d.Ratings.Get(updraft).Stars != 2 {
		t.Fatal("comma should take a star away")
	}
	// zen: keys act on the playing track, digits set stars, a adds to a playlist
	press(m, "z", "5", "l")
	playing := m.pl.State().Track.Path
	if r := m.d.Ratings.Get(playing); r.Stars != 5 || !r.Loved {
		t.Fatalf("zen rating: %+v", r)
	}
	must(t, screen(t, m), "♥ ★★★★★", "l love", "0-5 stars")
	press(m, "a")
	must(t, screen(t, m), "Add Origami to…", "New playlist")
	press(m, "enter")
	typeText(m, "Faves")
	press(m, "enter")
	if p, err := (playlist.Store{Dir: home.PlaylistsDir()}).Get("Faves"); err != nil || len(p.Entries) != 1 {
		t.Fatalf("%v %+v", err, p)
	}
	// loved songs section
	press(m, "z", "5")
	must(t, screen(t, m), "Loved", "Origami", "Updraft")
	// ratings survive a restart
	again, _ := ratings.Open(home.RatingsPath())
	if !again.Get(playing).Loved {
		t.Fatal("ratings.json not saved")
	}
	// the control socket can rate too
	if r := m.onIPC(ipc.Request{Cmd: "rate", Arg: "1"}); !r.OK || r.State.Track.Stars != 1 {
		t.Fatalf("%+v", r)
	}
}

// TestPixelArtFrame renders with the kitty renderer on, as in Rex: the
// frame must still be exactly the terminal's size.
func TestPixelArtFrame(t *testing.T) {
	m, eng := newTestModel(t, 64, 22)
	m.px = art.NewPixels()
	startPlaying(m, eng)
	out := m.View().Content
	if !strings.ContainsRune(out, '\U0010EEEE') {
		t.Fatal("no image placeholders drawn")
	}
	screen(t, m)
	press(m, "z")
	screen(t, m)
	if !m.px.Pending() {
		t.Fatal("covers should be queued for the terminal")
	}
}

// settle runs a command the way Bubble Tea would, feeding theme messages
// back until the palette has finished fading.
func settle(m *Model, cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for n := 0; len(queue) > 0 && n < 200; n++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case coverThemeMsg, fadeMsg:
			_, next := m.Update(msg)
			queue = append(queue, next)
		}
	}
}

func TestDynamicThemeFollowsTheCover(t *testing.T) {
	m, eng := newTestModel(t, 120, 34)
	nebula := m.pal.T.Bg

	// Give Paper Skies a cover: deep teal with a hot pink sun.
	var origami library.Track
	for _, tr := range m.lib.All() {
		if tr.Title == "Origami" {
			origami = tr
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, 120, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 120; x++ {
			c := color.RGBA{8, 70, 80, 255}
			if (x-60)*(x-60)+(y-60)*(y-60) < 900 {
				c = color.RGBA{255, 60, 150, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	os.MkdirAll(filepath.Dir(origami.Path), 0o755)
	f, _ := os.Create(filepath.Join(filepath.Dir(origami.Path), "cover.png"))
	png.Encode(f, img)
	f.Close()

	// On, and saved.
	press(m, "c")
	if cfg, _ := home.Load(); !cfg.DynamicTheme {
		t.Fatal("c should save dynamic_theme in config.json")
	}
	if b, _ := os.ReadFile(home.ConfigPath()); !strings.Contains(string(b), `"dynamic_theme": true`) {
		t.Fatalf("config.json should spell it dynamic_theme:\n%s", b)
	}
	if m.pal.T.Bg != nebula {
		t.Fatal("with nothing playing the base theme stays")
	}

	// Playing a song with art fades the whole palette to the cover's.
	startPlaying(m, eng)
	m.coverBusy = "" // startPlaying dropped the command that derives the theme; ask again
	settle(m, m.retheme(false))
	settle(m, m.retheme(false))
	if m.pal.T.Name != "cover" || !m.coverOn || m.pal.T.Bg == nebula {
		t.Fatalf("palette should come from the cover, got %q bg %s", m.pal.T.Name, m.pal.T.Bg)
	}
	if v := m.View(); v.BackgroundColor != m.pal.Bg {
		t.Error("the terminal background should take the cover's colour")
	}
	press(m, "z")
	must(t, screen(t, m), "Origami", "cover colours")

	// A fresh model (a restart) keeps the setting.
	cfg, _ := home.Load()
	if !cfg.DynamicTheme {
		t.Fatal("setting lost")
	}

	// Off again: back to nebula.
	m.toggleDynamic()
	settle(m, m.retheme(false))
	if m.pal.T.Name != "nebula" || m.coverOn {
		t.Fatalf("c again should restore the base theme, got %q", m.pal.T.Name)
	}
	if cfg, _ := home.Load(); cfg.DynamicTheme {
		t.Fatal("off should be saved too")
	}

	// The theme picker offers it first, previews it, and esc reverts.
	press(m, "z", "T")
	out := screen(t, m)
	must(t, out, "cover", "colours from the playing song's art", "terminal")
	m.modal.(*themePicker).sel = 1
	press(m, "k") // up to "cover"
	settle(m, m.retheme(false))
	if !m.cfg.DynamicTheme || m.pal.T.Name != "cover" {
		t.Fatalf("previewing cover should show it, got %q", m.pal.T.Name)
	}
	press(m, "esc")
	if m.cfg.DynamicTheme || m.pal.T.Name != "nebula" {
		t.Fatalf("esc should revert, got %q dynamic=%v", m.pal.T.Name, m.cfg.DynamicTheme)
	}
}

func TestDynamicThemeWithoutArtKeepsTheBase(t *testing.T) {
	m, eng := newTestModel(t, 100, 30)
	press(m, "c")
	startPlaying(m, eng)
	m.coverBusy = "" // startPlaying dropped the command that derives the theme; ask again
	settle(m, m.retheme(false))
	settle(m, m.retheme(false))
	if m.pal.T.Name != "nebula" || m.coverOn {
		t.Fatalf("a song without art keeps the base theme, got %q", m.pal.T.Name)
	}
	screen(t, m)
}

func TestSoundModesPreviewLiveAndSave(t *testing.T) {
	m, eng := newTestModel(t, 120, 34)
	startPlaying(m, eng)

	press(m, "m")
	must(t, screen(t, m), "Sound", "pure", "punch", "wide", "deep", "laptop speakers", "under full scale")
	press(m, "down", "down") // pure → punch → wide, heard as the cursor moves
	if eng.af != sound.Filter("wide") {
		t.Fatalf("moving should switch the sound live, engine has %q", eng.af)
	}
	press(m, "esc")
	if eng.af != "" || m.cfg.Audio.Sound != "" {
		t.Fatalf("esc should go back to pure, engine has %q", eng.af)
	}

	press(m, "m", "down", "down", "enter")
	if cfg, _ := home.Load(); cfg.Audio.Sound != "wide" {
		t.Fatalf("enter should save audio.sound, got %q", cfg.Audio.Sound)
	}
	press(m, "z")
	must(t, screen(t, m), "◆ wide", "m sound")

	// From outside: by name, and the next one without a name.
	if r := m.onIPC(ipc.Request{Cmd: "sound", Arg: "deep"}); r.Error != "" || eng.af != sound.Filter("deep") {
		t.Fatalf("ctl sound deep: %+v, engine %q", r, eng.af)
	}
	if r := m.onIPC(ipc.Request{Cmd: "sound"}); r.Error != "" || eng.af != "" {
		t.Fatalf("ctl sound should wrap from deep to pure: %+v, engine %q", r, eng.af)
	}
	if r := m.onIPC(ipc.Request{Cmd: "sound", Arg: "loud"}); r.Error == "" {
		t.Fatal("unknown modes should be refused")
	}
}

func TestMediaKeysDriveTheQueue(t *testing.T) {
	m, eng := newTestModel(t, 100, 30)
	startPlaying(m, eng)
	first := eng.playing
	key := func(cmd string) { m.Update(engineMsg(engine.Event{Kind: engine.Remote, Command: cmd})) }

	key(engine.RemoteNext)
	if eng.playing == first {
		t.Fatal("next key should move to the next track in kyro's queue")
	}
	m.Update(engineMsg(engine.Event{Kind: engine.Started, Path: eng.playing}))
	key(engine.RemotePrev) // just started: back one
	if eng.playing != first {
		t.Fatalf("prev key should go back to %s, playing %s", first, eng.playing)
	}
	key(engine.RemoteStop)
	if m.pl.State().Active {
		t.Fatal("stop key should stop")
	}
	key(engine.RemotePlay)
	if !m.pl.State().Active {
		t.Fatal("play key should start the queue again")
	}
}

func TestPlaysAreRecordedForTheYearInReview(t *testing.T) {
	m, eng := newTestModel(t, 120, 34)
	key := make([]byte, 32) // tests never touch the real keychain
	st, err := stats.Open(filepath.Join(t.TempDir(), "listening.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	m.d.Stats = st
	ev := func(e engine.Event) { m.Update(engineMsg(e)) }

	startPlaying(m, eng) // Origami, from Songs; startPlaying sits at 1:23
	first := eng.playing
	for s := 84; s <= 100; s++ {
		ev(engine.Event{Kind: engine.Position, Pos: time.Duration(s) * time.Second})
	}
	ev(engine.Event{Kind: engine.Paused, Paused: true})
	press(m, "l") // love it: an event
	ev(engine.Event{Kind: engine.Paused, Paused: false})

	press(m, "]") // skip to the next song
	ev(engine.Event{Kind: engine.Ended, Path: first, Reason: engine.Stopped})
	ev(engine.Event{Kind: engine.Started, Path: eng.playing})
	for s := 1; s <= 5; s++ {
		ev(engine.Event{Kind: engine.Position, Pos: time.Duration(s) * time.Second})
	}
	m.quit() // closing kyro ends the play in progress
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, _ = stats.Open(st.Path(), key)
	defer st.Close()
	plays, err := st.Plays(time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(plays) != 2 {
		t.Fatalf("want 2 plays, got %+v", plays)
	}
	a, b := plays[0], plays[1]
	if a.Title != "Origami" || a.EndReason != stats.Skipped || a.Context != "songs" || a.Pauses != 1 {
		t.Errorf("first play: %+v", a)
	}
	if a.Listened < 16*time.Second || a.Listened > 18*time.Second || a.Codec == "" {
		t.Errorf("first play listened %v (want about 17 s): %+v", a.Listened, a)
	}
	if b.EndReason != stats.Quit || b.Listened != 5*time.Second {
		t.Errorf("second play: %+v", b)
	}
	sum, _ := st.Summarize(time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if sum.EventCount != 1 {
		t.Errorf("the love should be logged, events %d", sum.EventCount)
	}
}

func TestWebPlayerAndOutputs(t *testing.T) {
	m, eng := newTestModel(t, 110, 34)
	router := engine.NewRouter("local", "Mac", eng, 70, nil)
	tv := &fakeEngine{}
	router.Add("tv-12345678", "TV", tv)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	srv := web.New(web.Deps{Library: m.lib, Router: router, Dir: home.WebDir(), Do: m.onIPC, Host: "127.0.0.1"})
	t.Cleanup(func() { srv.Stop() })
	m.d.Engine, m.d.Router, m.d.Web = router, router, srv
	m.pl = player.New(router, 70, false, player.RepeatOff)
	m.cfg.Web.Port = port

	// w turns the web player on, saves it, and opens pairing while shown.
	press(m, "w")
	press(m, "enter")
	if !srv.Running() || srv.PairingCode() == "" {
		t.Fatal("web player should run and offer a pairing code")
	}
	if cfg, _ := home.Load(); !cfg.Web.Enabled {
		t.Fatal("web player setting not saved")
	}
	screen(t, m)
	press(m, "esc")
	if srv.PairingCode() != "" {
		t.Fatal("pairing must close with the dialog")
	}

	// Pages play by track ID.
	ids := []string{m.tracks[3].ID, m.tracks[4].ID}
	if r := m.onIPC(ipc.Request{Cmd: "play-tracks", IDs: ids, Index: 1, Arg: "album:Paper Skies"}); !r.OK {
		t.Fatalf("play-tracks: %+v", r)
	}
	if s := m.pl.State(); s.Track.ID != ids[1] || m.playCtx != [2]string{"album", "Paper Skies"} {
		t.Fatalf("playing %q from %v", s.Track.Title, m.playCtx)
	}
	if r := m.onIPC(ipc.Request{Cmd: "jump", Index: 0}); !r.OK || m.pl.State().Track.ID != ids[0] {
		t.Fatalf("jump: %+v", r)
	}
	if r := m.onIPC(ipc.Request{Cmd: "output", Arg: "gone"}); r.OK {
		t.Fatal("switching to a missing output should fail")
	}

	// o moves the song to the TV; the player shows where it plays.
	press(m, "o", "down", "enter")
	if router.Active() != "tv-12345678" || tv.playing != eng.playing {
		t.Fatalf("active %q, tv playing %q", router.Active(), tv.playing)
	}
	if !strings.Contains(screen(t, m), "⇢ TV") {
		t.Fatal("the player should say it plays on the TV")
	}
	st := m.webState()
	if st.Output != "tv-12345678" || len(st.Outputs) != 2 || st.Track == nil || st.Track.ID != ids[0] || st.Theme.Bg == "" || st.QueueLen != 2 {
		t.Fatalf("web state %+v", st)
	}
}

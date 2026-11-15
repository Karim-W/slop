// kyro is a local music player for the terminal: a library that references
// your files where they live, playlists kept as .m3u8, cover art, hooks
// for other programs, and a layout that fits anything from a full window to
// a small tiling pane.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/kyro/internal/applemusic"
	"github.com/karim-w/slop/apps/kyro/internal/engine"
	"github.com/karim-w/slop/apps/kyro/internal/home"
	"github.com/karim-w/slop/apps/kyro/internal/hooks"
	"github.com/karim-w/slop/apps/kyro/internal/ipc"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/apps/kyro/internal/playlist"
	"github.com/karim-w/slop/apps/kyro/internal/ratings"
	"github.com/karim-w/slop/apps/kyro/internal/stats"
	"github.com/karim-w/slop/apps/kyro/internal/ui"
	"github.com/karim-w/slop/apps/kyro/internal/web"
	"github.com/karim-w/slop/pkg/domain/buildinfo"
	"github.com/karim-w/slop/pkg/domain/theme"
	"github.com/karim-w/slop/pkg/infra/keychain"
	"github.com/karim-w/slop/pkg/infra/paths"
)

const usage = `kyro %s — a music player for the terminal

usage:
  kyro [--theme NAME]            open the player
  kyro import PATH...            add folders or files (or .m3u8 playlists)
  kyro scan                      re-index every source
  kyro sources [rm PATH]         list or remove sources
  kyro status [--json | --format TEMPLATE]
                                 what is playing, e.g. --format '{{.Artist}} - {{.Title}}'
  kyro ctl COMMAND [ARG]         toggle, play, pause, next, prev, stop,
                                 seek ±SECONDS, volume ±N, shuffle, repeat,
                                 love, rate 0-5, sound [MODE],
                                 output local|NAME (where the song plays)
  kyro listen                    stream playback events as JSON lines
  kyro playlists                 list playlists
  kyro playlist export NAME DEST copy a playlist out (iTunes / Music can import it)
  kyro music pull | push         copy loves and stars from / to Apple Music
  kyro stats [YEAR]              what your encrypted listening stats hold
  kyro devices                   list audio output devices
  kyro doctor                    check the setup
  kyro themes | version

everything lives in %s
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kyro:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	if err := home.Ensure(); err != nil {
		return err
	}
	switch cmd {
	case "":
		return tui(args)
	case "import":
		return importCmd(args)
	case "scan":
		return scanCmd()
	case "sources":
		return sourcesCmd(args)
	case "status":
		return statusCmd(args)
	case "ctl":
		return ctlCmd(args)
	case "listen":
		return ipc.Subscribe(home.SocketPath(), func(e hooks.Event) bool {
			b, _ := json.Marshal(e)
			fmt.Println(string(b))
			return true
		})
	case "playlists":
		return playlistsCmd()
	case "playlist":
		return playlistCmd(args)
	case "devices":
		return devicesCmd()
	case "music":
		return musicCmd(args)
	case "doctor":
		return doctorCmd()
	case "stats":
		return statsCmd(args)
	case "themes":
		fmt.Println("terminal")
		for _, t := range theme.All(home.ThemesDir()) {
			fmt.Println(t.Name)
		}
		return nil
	case "version":
		fmt.Println("kyro", buildinfo.Get())
		return nil
	case "help", "-h", "--help":
		fmt.Printf(usage, buildinfo.Get(), home.Dir())
		return nil
	}
	return fmt.Errorf("unknown command %q (try kyro help)", cmd)
}

// ─── The player ─────────────────────────────────────────────────────────────

func tui(args []string) error {
	fs := flag.NewFlagSet("kyro", flag.ContinueOnError)
	themeName := fs.String("theme", os.Getenv("KYRO_THEME"), `theme, or "terminal" to follow the terminal's colours`)
	ver := fs.Bool("version", false, "print version")
	fs.Usage = func() { fmt.Fprintf(os.Stderr, usage, buildinfo.Get(), home.Dir()) }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *ver {
		fmt.Println("kyro", buildinfo.Get())
		return nil
	}

	log, closeLog := openLog()
	defer closeLog()
	cfg, err := home.Load()
	if err != nil {
		return fmt.Errorf("reading %s: %w", home.ConfigPath(), err)
	}
	lib, err := library.Open(home.LibraryPath())
	if err != nil {
		return fmt.Errorf("reading %s: %w", home.LibraryPath(), err)
	}

	var local engine.Engine
	mpv, engErr := engine.StartMpv(cfg.Audio, cfg.Volume)
	if engErr == nil {
		local = mpv
	} else {
		log.Error("audio engine", "err", engErr)
	}
	// The router plays through this computer or a browser speaker.
	name := computerName(cfg.Web.Name)
	router := engine.NewRouter("local", name, local, cfg.Volume, engErr)

	rs, err := ratings.Open(home.RatingsPath())
	if err != nil {
		return fmt.Errorf("reading %s: %w", home.RatingsPath(), err)
	}
	var st *stats.Store
	if cfg.Stats != "off" {
		if st, err = openStats(); err != nil {
			log.Warn("listening stats are off", "err", err)
		}
		defer func() {
			if err := st.Close(); err != nil {
				log.Warn("listening stats", "err", err)
			}
		}()
	}
	disp := &hooks.Dispatcher{Dir: home.HooksDir(), Commands: cfg.Hooks, StatePath: home.NowPlayingPath(), Log: log}
	var prog *tea.Program
	// do runs a control command on the UI goroutine, which owns the player.
	do := func(r ipc.Request) ipc.Response {
		reply := make(chan ipc.Response, 1)
		prog.Send(ui.IPCMsg{Req: r, Reply: reply})
		select {
		case resp := <-reply:
			return resp
		case <-time.After(3 * time.Second):
			return ipc.Response{Error: "kyro is busy"}
		}
	}
	mpvBin, _ := engine.MpvPath(cfg.Audio.Mpv)
	webSrv := web.New(web.Deps{
		Library: lib, Playlists: playlist.Store{Dir: home.PlaylistsDir()}, Ratings: rs, Router: router, Do: do,
		Mpv: mpvBin, Dir: home.WebDir(), CacheDir: home.WebCacheDir(), Name: name, Log: log,
	})
	model := ui.New(ui.Deps{
		Config: cfg, Library: lib, Playlists: playlist.Store{Dir: home.PlaylistsDir()},
		Engine: router, EngineErr: engErr, Hooks: disp, Ratings: rs, Stats: st, Log: log, ThemeName: *themeName,
		Router: router, Web: webSrv,
	})
	prog = tea.NewProgram(model)

	srv, err := ipc.Listen(home.SocketPath(), do)
	switch {
	case errors.Is(err, ipc.ErrRunning):
		log.Warn("control socket in use by another kyro; this one runs without it")
	case err != nil:
		log.Warn("control socket", "err", err)
	default:
		disp.Publish = srv.Broadcast
		defer srv.Close()
	}

	_, err = prog.Run()
	webSrv.Stop()
	router.Close()
	disp.Wait()
	return err
}

// computerName is what the web player calls this computer: the name in
// config, else the one in System Settings, else the host name.
func computerName(configured string) string {
	if configured != "" {
		return configured
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("scutil", "--get", "ComputerName").Output(); err == nil {
			if n := strings.TrimSpace(string(out)); n != "" {
				return n
			}
		}
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return strings.TrimSuffix(h, ".local")
	}
	return "This computer"
}

// openLog writes to ~/.kyro/kyro.log when KYRO_LOG is set (a TUI owns the
// terminal, so logs can't go there).
func openLog() (*slog.Logger, func()) {
	level := os.Getenv("KYRO_LOG")
	if level == "" {
		return slog.New(slog.NewTextHandler(io.Discard, nil)), func() {}
	}
	f, err := os.OpenFile(home.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil)), func() {}
	}
	var l slog.Level
	if l.UnmarshalText([]byte(level)) != nil {
		l = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: l})), func() { f.Close() }
}

// ─── Library commands ───────────────────────────────────────────────────────

func importCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: kyro import PATH...")
	}
	cfg, err := home.Load()
	if err != nil {
		return err
	}
	store := playlist.Store{Dir: home.PlaylistsDir()}
	for _, a := range args {
		abs, err := filepath.Abs(paths.Expand(a))
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); err != nil {
			return err
		}
		if ext := strings.ToLower(filepath.Ext(abs)); ext == ".m3u8" || ext == ".m3u" {
			p, err := store.Import(abs)
			if err != nil {
				return err
			}
			fmt.Printf("imported playlist %q (%d entries)\n", p.Name, len(p.Entries))
			continue
		}
		if !slices.Contains(cfg.Sources, abs) {
			cfg.Sources = append(cfg.Sources, abs)
		}
	}
	if err := home.Save(cfg); err != nil {
		return err
	}
	return scanCmd()
}

func scanCmd() error {
	cfg, err := home.Load()
	if err != nil {
		return err
	}
	lib, err := library.Open(home.LibraryPath())
	if err != nil {
		return err
	}
	isTTY := isTerminal(os.Stdout)
	res := lib.Scan(cfg.Sources, func(n int) {
		if isTTY {
			fmt.Printf("\rscanning… %d files", n)
		}
	})
	if isTTY {
		fmt.Print("\r\033[K")
	}
	if err := lib.Save(); err != nil {
		return err
	}
	fmt.Printf("%d tracks · %d new · %d updated · %d missing\n", res.Total, res.Added, res.Updated, res.Missing)
	for _, e := range res.Errors[:min(len(res.Errors), 5)] {
		fmt.Println("  !", e)
	}
	return nil
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func sourcesCmd(args []string) error {
	cfg, err := home.Load()
	if err != nil {
		return err
	}
	if len(args) == 2 && args[0] == "rm" {
		abs, _ := filepath.Abs(paths.Expand(args[1]))
		i := slices.Index(cfg.Sources, abs)
		if i < 0 {
			return fmt.Errorf("%s is not a source", abs)
		}
		cfg.Sources = slices.Delete(cfg.Sources, i, i+1)
		lib, err := library.Open(home.LibraryPath())
		if err != nil {
			return err
		}
		n := lib.RemoveSource(abs)
		if err := lib.Save(); err != nil {
			return err
		}
		fmt.Printf("removed %s (%d tracks; files untouched)\n", abs, n)
		return home.Save(cfg)
	}
	if len(cfg.Sources) == 0 {
		fmt.Println("no sources yet: kyro import ~/Music")
	}
	for _, s := range cfg.Sources {
		fmt.Println(s)
	}
	return nil
}

// ─── Control ────────────────────────────────────────────────────────────────

func statusCmd(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the full state as JSON")
	format := fs.String("format", "", "Go template over the track, e.g. '{{.Artist}} - {{.Title}}'")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resp, err := ipc.Call(home.SocketPath(), ipc.Request{Cmd: "status"})
	if err != nil {
		// not running: fall back to the last known state on disk
		var e hooks.Event
		b, rerr := os.ReadFile(home.NowPlayingPath())
		if rerr != nil || json.Unmarshal(b, &e) != nil {
			return err
		}
		e.Playing = false
		resp.State = &e
	}
	st := resp.State
	switch {
	case *asJSON:
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(b))
	case *format != "":
		if st.Track == nil {
			return nil
		}
		t, err := template.New("f").Parse(*format)
		if err != nil {
			return err
		}
		if err := t.Execute(os.Stdout, st.Track); err != nil {
			return err
		}
		fmt.Println()
	default:
		if st.Track == nil {
			fmt.Println("nothing playing")
			return nil
		}
		icon := "❚❚"
		if st.Playing {
			icon = "▶"
		}
		fmt.Printf("%s %s — %s  (%s / %s)\n", icon, st.Track.Title, st.Track.Artist,
			clock(st.Position), clock(st.Track.Duration))
	}
	return nil
}

func clock(s float64) string {
	d := time.Duration(s * float64(time.Second)).Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func ctlCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: kyro ctl toggle|play|pause|next|prev|stop|seek ±S|volume ±N|shuffle|repeat|love|rate N|sound [MODE]|output local|NAME")
	}
	req := ipc.Request{Cmd: args[0]}
	if len(args) > 1 {
		req.Arg = args[1]
	}
	_, err := ipc.Call(home.SocketPath(), req)
	return err
}

// ─── Playlists ──────────────────────────────────────────────────────────────

func playlistsCmd() error {
	ps, err := playlist.Store{Dir: home.PlaylistsDir()}.List()
	if err != nil {
		return err
	}
	for _, p := range ps {
		fmt.Printf("%-30s %4d tracks  %s\n", p.Name, len(p.Entries), p.File)
	}
	return nil
}

func playlistCmd(args []string) error {
	if len(args) == 3 && args[0] == "export" {
		out, err := playlist.Store{Dir: home.PlaylistsDir()}.Export(args[1], paths.Expand(args[2]))
		if err != nil {
			return err
		}
		fmt.Println("exported", out)
		return nil
	}
	return errors.New("usage: kyro playlist export NAME DEST")
}

// ─── Setup ──────────────────────────────────────────────────────────────────

func devicesCmd() error {
	cfg, _ := home.Load()
	devs, err := engine.Devices(cfg.Audio)
	if err != nil {
		return err
	}
	for _, d := range devs {
		mark := "  "
		if d[0] == cfg.Audio.Device || cfg.Audio.Device == "" && d[0] == "auto" {
			mark = "* "
		}
		fmt.Printf("%s%-50s %s\n", mark, d[0], d[1])
	}
	fmt.Println("\nset one with \"audio\": {\"device\": \"…\"} in", home.ConfigPath())
	return nil
}

func doctorCmd() error {
	cfg, err := home.Load()
	check := func(ok bool, what, detail string) {
		mark := "✓"
		if !ok {
			mark = "✗"
		}
		fmt.Printf("%s %-14s %s\n", mark, what, detail)
	}
	check(err == nil, "config", home.ConfigPath())
	mpv, merr := engine.MpvPath(cfg.Audio.Mpv)
	check(merr == nil, "mpv", orErr(mpv, merr))
	lib, lerr := library.Open(home.LibraryPath())
	n := 0
	if lerr == nil {
		n = len(lib.Tracks)
	}
	check(lerr == nil, "library", fmt.Sprintf("%d tracks from %d sources", n, len(cfg.Sources)))
	for _, s := range cfg.Sources {
		_, err := os.Stat(s)
		check(err == nil, "  source", s)
	}
	safe := !cfg.Audio.Exclusive && len(cfg.Audio.Passthrough) == 0 && cfg.Volume <= 100
	check(safe, "audio", fmt.Sprintf("shared mode, channels=%s, volume %d%%", cfg.Audio.Channels, cfg.Volume))
	_, serr := ipc.Call(home.SocketPath(), ipc.Request{Cmd: "status"})
	check(true, "running", map[bool]string{true: "yes (" + home.SocketPath() + ")", false: "no"}[serr == nil])
	hooksFound, _ := filepath.Glob(filepath.Join(home.HooksDir(), "*"))
	check(true, "hooks", fmt.Sprintf("%d executables in %s, %d in config", len(hooksFound), home.HooksDir(), len(cfg.Hooks)))
	return nil
}

func orErr(s string, err error) string {
	if err != nil {
		return err.Error()
	}
	return s
}

// ─── Apple Music ────────────────────────────────────────────────────────────

func musicCmd(args []string) error {
	if len(args) == 0 || args[0] != "pull" && args[0] != "push" && args[0] != "status" {
		return errors.New("usage: kyro music pull | push | status")
	}
	if !applemusic.Available() {
		return applemusic.ErrUnsupported
	}
	rs, err := ratings.Open(home.RatingsPath())
	if err != nil {
		return err
	}
	switch args[0] {
	case "status":
		fmt.Printf("Music running: %v · %d ratings waiting to sync\n", applemusic.Running(), len(rs.Unsynced()))
		return nil
	case "pull":
		fmt.Println("reading loves and ratings from Music (this opens Music)…")
		rated, err := applemusic.Pull(true)
		if err != nil {
			return err
		}
		lib, err := library.Open(home.LibraryPath())
		if err != nil {
			return err
		}
		known, kept := 0, 0
		for _, r := range rated {
			if _, ok := lib.ByPath(r.Path); ok {
				known++
			}
			// a rating changed in kyro and not yet pushed wins
			if cur := rs.Get(r.Path); !cur.Synced && (cur.Loved || cur.Stars > 0) {
				kept++
				continue
			}
			rs.Import(r.Path, r.Stars, r.Loved)
		}
		if err := rs.Save(); err != nil {
			return err
		}
		fmt.Printf("%d rated or loved tracks in Music (%d in kyro's library); kept %d newer kyro ratings\n", len(rated), known, kept)
		return nil
	}
	pending := rs.Unsynced()
	fmt.Printf("pushing %d ratings to Music (this opens Music)…\n", len(pending))
	lib, _ := library.Open(home.LibraryPath())
	done, missing := 0, 0
	for _, p := range pending {
		r := rs.Get(p)
		title := ""
		if t, ok := lib.ByPath(p); ok {
			title = t.Title
		}
		switch err := applemusic.Push(p, title, r.Stars, r.Loved, true); {
		case errors.Is(err, applemusic.ErrNotFound):
			missing++
			rs.MarkSynced(p)
		case err != nil:
			rs.Save()
			return err
		default:
			done++
			rs.MarkSynced(p)
		}
	}
	fmt.Printf("synced %d · %d aren't in your Music library\n", done, missing)
	return rs.Save()
}

// ─── Listening stats ────────────────────────────────────────────────────────

// openStats opens the encrypted stats database with its key from the
// keychain (made the first time).
func openStats() (*stats.Store, error) {
	key, err := stats.LoadKey(keychain.New(stats.KeychainService), home.StatsPath())
	if err != nil {
		return nil, err
	}
	return stats.Open(home.StatsPath(), key)
}

func statsCmd(args []string) error {
	year := time.Now().Year()
	if len(args) > 0 {
		y, err := strconv.Atoi(args[0])
		if err != nil {
			return errors.New("usage: kyro stats [YEAR]")
		}
		year = y
	}
	if cfg, _ := home.Load(); cfg.Stats == "off" {
		fmt.Println(`listening stats are off ("stats": "off" in config.json)`)
	}
	st, err := openStats()
	if err != nil {
		return err
	}
	defer st.Close()
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.Local)
	sum, err := st.Summarize(from, from.AddDate(1, 0, 0))
	if err != nil {
		return err
	}
	fmt.Printf("%d so far\n", year)
	fmt.Printf("  plays        %d (of %d recorded, skips included)\n", sum.Plays, sum.AllPlays)
	fmt.Printf("  listened     %s\n", sum.Listened.Round(time.Minute))
	fmt.Printf("  songs        %d different\n", sum.Songs)
	if sum.TopArtist != "" {
		fmt.Printf("  top artist   %s\n", sum.TopArtist)
		fmt.Printf("  top song     %s (%d plays)\n", sum.TopSong, sum.TopSongN)
	}
	fmt.Printf("  events       %d loves, ratings and playlist adds\n", sum.EventCount)
	if !sum.Since.IsZero() {
		fmt.Printf("\nrecording since %s\n", sum.Since.Format("2 Jan 2006"))
	}
	fmt.Printf("stored encrypted in %s\nkey in the keychain: %s / %s\n", st.Path(), stats.KeychainService, stats.KeychainAccount)
	return nil
}

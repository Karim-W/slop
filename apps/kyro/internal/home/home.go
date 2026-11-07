// Package home is kyro's own folder, ~/.kyro (or $KYRO_HOME), and the
// settings in its config.json:
//
//	~/.kyro/
//	  config.json        settings, music sources, hooks
//	  library.json       index of tracks (references to your files, never copies)
//	  playlists/*.m3u8   playlists, ready to import into iTunes / Music
//	  hooks/             executables run on playback events
//	  themes/*.json      extra themes
//	  now-playing.json   the current track, rewritten on every change
//	  cache/art/         cover art extracted for hooks and notifications
package home

import (
	"os"
	"path/filepath"

	"github.com/karim-w/slop/pkg/infra/jsonfile"
)

// Dir is kyro's folder: $KYRO_HOME, or ~/.kyro.
func Dir() string {
	if d := os.Getenv("KYRO_HOME"); d != "" {
		return d
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ".kyro"
	}
	return filepath.Join(h, ".kyro")
}

func ConfigPath() string     { return filepath.Join(Dir(), "config.json") }
func LibraryPath() string    { return filepath.Join(Dir(), "library.json") }
func PlaylistsDir() string   { return filepath.Join(Dir(), "playlists") }
func HooksDir() string       { return filepath.Join(Dir(), "hooks") }
func ThemesDir() string      { return filepath.Join(Dir(), "themes") }
func NowPlayingPath() string { return filepath.Join(Dir(), "now-playing.json") }
func ArtCacheDir() string    { return filepath.Join(Dir(), "cache", "art") }
func RatingsPath() string    { return filepath.Join(Dir(), "ratings.json") }
func LogPath() string        { return filepath.Join(Dir(), "kyro.log") }

// SocketPath is the control socket other programs talk to. $KYRO_SOCKET
// overrides it (Unix socket paths are limited to ~100 bytes).
func SocketPath() string {
	if s := os.Getenv("KYRO_SOCKET"); s != "" {
		return s
	}
	return filepath.Join(Dir(), "kyro.sock")
}

// Ensure creates the folder layout.
func Ensure() error {
	for _, d := range []string{Dir(), PlaylistsDir(), HooksDir(), ThemesDir(), ArtCacheDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// Config is config.json.
type Config struct {
	// Theme is a built-in or user theme name. Empty follows the terminal's
	// colours inside Rex and uses nebula elsewhere; "terminal" always follows.
	Theme string `json:"theme"`
	// Sources are the folders and files kyro indexes. Files stay where they
	// are; the library only stores their paths.
	Sources []string `json:"sources"`
	Audio   Audio    `json:"audio"`
	// Volume is 0–100. kyro never raises it above 100.
	Volume  int    `json:"volume"`
	Shuffle bool   `json:"shuffle"`
	Repeat  string `json:"repeat"` // "off", "all" or "one"
	// Art picks how cover art is drawn: "auto" (default: real pixels where
	// the terminal supports the kitty graphics protocol, as Rex, kitty,
	// Ghostty and WezTerm do, else quadrant blocks), "pixels", "sextant"
	// (2×3 blocks, needs a font with Unicode 13 sextants), "quadrant" or
	// "halfblock".
	Art string `json:"art"`
	// AppleMusic mirrors loves and stars into the macOS Music app: "auto"
	// (default) pushes while Music is open and catches up later otherwise,
	// "always" opens Music if needed, "off" never touches it.
	AppleMusic string `json:"appleMusic"`
	// DynamicTheme takes the whole theme (background, text, accents,
	// gradients) from the cover of the playing song. Toggle it with c.
	DynamicTheme bool `json:"dynamic_theme"`
	// Hooks run commands on playback events, in addition to executables in
	// ~/.kyro/hooks.
	Hooks []Hook `json:"hooks,omitempty"`
}

// Audio controls the output. The defaults go through the OS mixer like any
// other app, which is always safe for the sound card and drivers.
type Audio struct {
	// Device is an mpv audio device ("" = the system default). List them
	// with `kyro devices`.
	Device string `json:"device,omitempty"`
	// Channels is mpv's channel policy. "auto-safe" (default) plays surround
	// only when the device reports it can take it, otherwise downmixes.
	// "stereo" always downmixes; "auto" sends every channel.
	Channels string `json:"channels"`
	// Exclusive takes the device for bit-perfect playback, so no other app
	// can play sound meanwhile. Off by default.
	Exclusive bool `json:"exclusive"`
	// Passthrough sends these compressed formats (ac3, eac3, dts, dts-hd,
	// truehd) untouched to an S/PDIF or HDMI receiver. Only enable it if
	// your receiver decodes them. Off by default.
	Passthrough []string `json:"passthrough,omitempty"`
	// Sound is the sound mode: "pure" (default: untouched), "punch",
	// "wide" (for laptop speakers) or "deep" (headphones). Every mode keeps
	// peaks under full scale. Choose with m in the player.
	Sound string `json:"sound,omitempty"`
	// ReplayGain is "no" (default), "track" or "album".
	ReplayGain string `json:"replaygain"`
	// Mpv is the path to mpv ("" = find it on $PATH).
	Mpv string `json:"mpv,omitempty"`
	// Extra mpv flags, for experts.
	Extra []string `json:"extra,omitempty"`
}

// Hook runs Command (through sh -c) when Event happens. Event is one of
// track, pause, resume, stop, or "*" for all of them.
type Hook struct {
	Event   string `json:"event"`
	Command string `json:"command"`
}

// Default is the config a fresh install starts with.
func Default() Config {
	return Config{
		Volume:     70,
		Repeat:     "off",
		Art:        "auto",
		AppleMusic: "auto",
		Audio:      Audio{Channels: "auto-safe", ReplayGain: "no"},
	}
}

// Load reads config.json, filling anything missing with defaults.
func Load() (Config, error) {
	c := Default()
	if _, err := jsonfile.Load(ConfigPath(), &c); err != nil {
		return Default(), err
	}
	d := Default()
	if c.Audio.Channels == "" {
		c.Audio.Channels = d.Audio.Channels
	}
	if c.Audio.ReplayGain == "" {
		c.Audio.ReplayGain = d.Audio.ReplayGain
	}
	if c.Repeat == "" {
		c.Repeat = d.Repeat
	}
	if c.Art == "" {
		c.Art = d.Art
	}
	if c.AppleMusic == "" {
		c.AppleMusic = d.AppleMusic
	}
	c.Volume = min(max(c.Volume, 0), 100)
	return c, nil
}

// Save writes config.json.
func Save(c Config) error { return jsonfile.Save(ConfigPath(), c, 0o644) }

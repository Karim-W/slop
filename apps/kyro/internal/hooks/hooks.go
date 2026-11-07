// Package hooks tells other programs what kyro is playing.
//
// On every playback event kyro:
//
//  1. rewrites ~/.kyro/now-playing.json (atomically), for status bars that
//     poll a file;
//  2. runs each executable in ~/.kyro/hooks named after the event (track,
//     pause, resume, stop) or named "event" for all of them;
//  3. runs the matching commands from config.json's "hooks" list;
//  4. sends the event to clients subscribed over the control socket.
//
// Hooks get the event as JSON on stdin and as KYRO_* environment variables.
// They run in the background with a timeout, so a slow hook never holds up
// the music.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/home"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/pkg/infra/jsonfile"
)

// Timeout is how long a hook may run before it is killed.
const Timeout = 10 * time.Second

// Track is the track as hooks see it.
type Track struct {
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album"`
	AlbumArtist string  `json:"albumArtist,omitempty"`
	Genre       string  `json:"genre,omitempty"`
	Year        int     `json:"year,omitempty"`
	TrackNo     int     `json:"trackNumber,omitempty"`
	Duration    float64 `json:"duration"` // seconds
	Path        string  `json:"path"`
	Art         string  `json:"art,omitempty"` // path to the cover image, if any
	Quality     string  `json:"quality,omitempty"`
	Codec       string  `json:"codec,omitempty"`
	SampleRate  int     `json:"sampleRate,omitempty"`
	BitDepth    int     `json:"bitDepth,omitempty"`
	Channels    int     `json:"channels,omitempty"`
	Loved       bool    `json:"loved"`
	Stars       int     `json:"stars"` // 0–5
}

// Event is the payload hooks receive.
type Event struct {
	Event    string  `json:"event"` // track, pause, resume, stop
	Playing  bool    `json:"playing"`
	Track    *Track  `json:"track,omitempty"`
	Position float64 `json:"position"` // seconds
	Volume   int     `json:"volume"`
	Shuffle  bool    `json:"shuffle"`
	Repeat   string  `json:"repeat"`
	Time     string  `json:"time"`
}

// FromTrack converts a library track; art is the exported cover path.
func FromTrack(t library.Track, art string) *Track {
	return &Track{
		Title: t.Title, Artist: t.Artist, Album: t.Album, AlbumArtist: t.AlbumArtist,
		Genre: t.Genre, Year: t.Year, TrackNo: t.TrackNo, Duration: t.Duration.Seconds(),
		Path: t.Path, Art: art, Quality: t.Quality(), Codec: t.Codec,
		SampleRate: t.SampleRate, BitDepth: t.BitDepth, Channels: t.Channels,
	}
}

// Env renders the event as KYRO_* variables.
func (e Event) Env() []string {
	env := []string{
		"KYRO_EVENT=" + e.Event,
		"KYRO_PLAYING=" + strconv.FormatBool(e.Playing),
		"KYRO_POSITION=" + strconv.FormatFloat(e.Position, 'f', 1, 64),
		"KYRO_VOLUME=" + strconv.Itoa(e.Volume),
	}
	if t := e.Track; t != nil {
		env = append(env,
			"KYRO_TITLE="+t.Title, "KYRO_ARTIST="+t.Artist, "KYRO_ALBUM="+t.Album,
			"KYRO_PATH="+t.Path, "KYRO_ART="+t.Art, "KYRO_QUALITY="+t.Quality,
			"KYRO_DURATION="+strconv.FormatFloat(t.Duration, 'f', 1, 64),
			"KYRO_LOVED="+strconv.FormatBool(t.Loved), "KYRO_STARS="+strconv.Itoa(t.Stars),
		)
	}
	return env
}

// Dispatcher fans events out. It is safe for concurrent use.
type Dispatcher struct {
	Dir       string      // executables dir (~/.kyro/hooks)
	Commands  []home.Hook // from config.json
	StatePath string      // now-playing.json
	Log       *slog.Logger
	// Publish, if set, also receives every event (the control socket).
	Publish func(Event)

	mu      sync.Mutex
	running map[string]run
	seq     uint64
	wg      sync.WaitGroup
}

type run struct {
	id     uint64
	cancel context.CancelFunc
}

// Fire announces e. It returns at once; hooks run in the background.
func (d *Dispatcher) Fire(e Event) {
	if e.Time == "" {
		e.Time = time.Now().Format(time.RFC3339)
	}
	if d.StatePath != "" {
		if err := jsonfile.Save(d.StatePath, e, 0o644); err != nil && d.Log != nil {
			d.Log.Warn("writing now-playing", "err", err)
		}
	}
	if d.Publish != nil {
		d.Publish(e)
	}
	payload, _ := json.Marshal(e)
	for _, h := range d.hooksFor(e.Event) {
		d.start(h, e, payload)
	}
}

type hook struct {
	name string   // stable identity, for replacing an older run
	argv []string // program and arguments
}

func (d *Dispatcher) hooksFor(event string) []hook {
	var out []hook
	if d.Dir != "" {
		for _, name := range []string{event, "event"} {
			p := filepath.Join(d.Dir, name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				out = append(out, hook{name: p, argv: []string{p}})
			}
		}
	}
	for i, h := range d.Commands {
		if h.Event == event || h.Event == "*" || h.Event == "" {
			out = append(out, hook{name: fmt.Sprintf("config#%d", i), argv: []string{"/bin/sh", "-c", h.Command}})
		}
	}
	return out
}

// start runs a hook. If the same hook is still busy with an older event of
// the same kind (quick track skipping), the older run is cancelled: hooks
// see the latest state, not a backlog. A different kind (a pause right
// after a track change) never cancels a run.
func (d *Dispatcher) start(h hook, e Event, payload []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	h.name += "|" + e.Event
	d.mu.Lock()
	if d.running == nil {
		d.running = map[string]run{}
	}
	if prev, ok := d.running[h.name]; ok {
		prev.cancel()
	}
	d.seq++
	id := d.seq
	d.running[h.name] = run{id, cancel}
	d.mu.Unlock()

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer cancel()
		cmd := exec.CommandContext(ctx, h.argv[0], h.argv[1:]...)
		cmd.Env = append(os.Environ(), e.Env()...)
		cmd.Stdin = bytes.NewReader(payload)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		cmd.WaitDelay = time.Second
		err := cmd.Run()
		if d.Log != nil {
			if err != nil && ctx.Err() != context.Canceled {
				d.Log.Warn("hook failed", "hook", h.name, "event", e.Event, "err", err, "output", trim(out.String()))
			} else {
				d.Log.Debug("hook ran", "hook", h.name, "event", e.Event)
			}
		}
		d.mu.Lock()
		if r, ok := d.running[h.name]; ok && r.id == id {
			delete(d.running, h.name)
		}
		d.mu.Unlock()
	}()
}

// Wait blocks until running hooks finish (for tests and shutdown).
func (d *Dispatcher) Wait() { d.wg.Wait() }

func trim(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

package web

import (
	"slices"
	"sync"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/engine"
)

// Device is a browser acting as a speaker. It is an engine.Engine: the
// player drives it exactly like mpv. Commands queue up with a sequence
// number and reach the browser over its event stream, and again in the
// reply to each report it posts, until it acknowledges them. That way a
// phone whose stream the OS paused in the background still gets the next
// song when the current one ends.
type Device struct {
	id, name string
	pair     string // the paired browser it belongs to
	formats  map[string]bool
	s        *Server
	events   chan engine.Event

	mu    sync.Mutex
	af    string // the sound mode's filters, rendered into what it gets
	seq   int64
	cmds  []Cmd
	conns int       // open event streams
	seen  time.Time // last contact

	// The clock: the browser reports where it is only every ten seconds
	// (so a phone's radio can sleep); between reports kyro counts on.
	playing bool
	base    float64 // seconds into the song at baseAt
	baseAt  time.Time
}

// Cmd is one instruction for a browser speaker.
type Cmd struct {
	Seq    int64   `json:"seq"`
	Op     string  `json:"op"` // play, next, pause, seek, volume, stop
	Track  string  `json:"track,omitempty"`
	URL    string  `json:"url,omitempty"`
	At     float64 `json:"at"`
	Paused bool    `json:"paused"`
	Volume int     `json:"volume"`
	// Form says what the browser gets: the file as is, or a lossless copy
	// (converted, or carrying the sound mode).
	Form string `json:"form,omitempty"`
}

// report is what a browser speaker posts back.
type report struct {
	Kind      string  `json:"kind"` // started, pos, duration, paused, ended
	Track     string  `json:"track"`
	Pos       float64 `json:"pos"`
	Duration  float64 `json:"duration"`
	Paused    bool    `json:"paused"`
	Reason    string  `json:"reason"` // eof, error
	Error     string  `json:"error"`
	Continues bool    `json:"continues"`
}

// setClock records where the song is; pass playing as nil to keep it.
func (d *Device) setClock(pos float64, playing *bool) {
	d.mu.Lock()
	d.base, d.baseAt = max(pos, 0), time.Now()
	if playing != nil {
		d.playing = *playing
	}
	d.mu.Unlock()
}

func ptr(b bool) *bool { return &b }

// tick sends the counted position once a second while the song plays, so
// the terminal's clock and the listening stats run smoothly. It ends once
// the server no longer lists this speaker.
func (d *Device) tick() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		if d.s.device(d.id) != d {
			return
		}
		d.mu.Lock()
		playing, pos := d.playing, d.base+time.Since(d.baseAt).Seconds()
		d.mu.Unlock()
		if playing {
			select {
			case d.events <- engine.Event{Kind: engine.Position, Pos: secs(pos)}:
			default:
			}
		}
	}
}

func newDevice(s *Server, id, name string, formats []string) *Device {
	d := &Device{id: id, name: name, s: s, events: make(chan engine.Event, 64), seen: time.Now(), formats: map[string]bool{}}
	for _, f := range formats {
		d.formats[f] = true
	}
	return d
}

func (d *Device) push(c Cmd) error {
	d.mu.Lock()
	d.seq++
	c.Seq = d.seq
	d.cmds = append(d.cmds, c)
	if len(d.cmds) > 64 {
		d.cmds = d.cmds[len(d.cmds)-64:]
	}
	d.mu.Unlock()
	d.s.hub.command(d.id, c)
	return nil
}

// pending drops what the browser acknowledged and returns the rest.
func (d *Device) pending(ack int64) []Cmd {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cmds = slices.DeleteFunc(d.cmds, func(c Cmd) bool { return c.Seq <= ack })
	d.seen = time.Now()
	return slices.Clone(d.cmds)
}

func (d *Device) touch(conn int) {
	d.mu.Lock()
	d.conns += conn
	d.seen = time.Now()
	d.mu.Unlock()
}

// gone reports whether the browser has been out of touch longer than
// grace.
func (d *Device) gone(grace time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.conns == 0 && time.Since(d.seen) > grace
}

func (d *Device) url(path string) (string, string) {
	id := d.s.media.allow(path)
	return id, "/api/audio/" + id + "?d=" + d.id
}

func (d *Device) filter() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.af
}

// form describes what a song reaches the browser as.
func (d *Device) form(id string) string {
	t, ok := d.s.media.track(id)
	if !ok {
		return ""
	}
	target := "WAV"
	if d.formats["flac"] {
		target = "FLAC"
	}
	switch k := Kind(t); {
	case d.filter() != "":
		return "sound mode · lossless " + target
	case k != "" && d.formats[k]:
		return "original " + t.Codec
	default:
		return "converted to lossless " + target
	}
}

func (d *Device) Play(path string) error { return d.PlayFrom(path, 0, false) }

func (d *Device) PlayFrom(path string, at time.Duration, paused bool) error {
	id, u := d.url(path)
	d.setClock(at.Seconds(), ptr(false)) // counting starts when the browser says it plays
	return d.push(Cmd{Op: "play", Track: id, URL: u, At: at.Seconds(), Paused: paused, Form: d.form(id)})
}

func (d *Device) Next(path string) error {
	if path == "" {
		return d.push(Cmd{Op: "next"})
	}
	id, u := d.url(path)
	return d.push(Cmd{Op: "next", Track: id, URL: u, Form: d.form(id)})
}

func (d *Device) SetPaused(p bool) error { return d.push(Cmd{Op: "pause", Paused: p}) }
func (d *Device) Seek(at time.Duration) error {
	d.setClock(max(at, 0).Seconds(), nil)
	return d.push(Cmd{Op: "seek", At: max(at, 0).Seconds()})
}
func (d *Device) SetVolume(v int) error {
	return d.push(Cmd{Op: "volume", Volume: min(max(v, 0), 100)})
}

// SetFilter takes the sound mode, which reaches the browser from the next
// song on: kyro renders it into a lossless copy, limiter and all.
func (d *Device) SetFilter(af string) error {
	d.mu.Lock()
	d.af = af
	d.mu.Unlock()
	return nil
}
func (d *Device) Stop() error {
	d.setClock(0, ptr(false))
	return d.push(Cmd{Op: "stop"})
}

func (d *Device) Close() error { return d.Stop() }

func (d *Device) Events() <-chan engine.Event { return d.events }

// apply turns a browser's reports into engine events.
func (d *Device) apply(rs []report) {
	for _, r := range rs {
		path := d.s.media.path(r.Track)
		var e engine.Event
		switch r.Kind {
		case "started":
			if path == "" {
				continue
			}
			e = engine.Event{Kind: engine.Started, Path: path}
			d.setClock(r.Pos, nil)
		case "pos":
			d.setClock(r.Pos, nil)
			e = engine.Event{Kind: engine.Position, Pos: secs(r.Pos)}
		case "duration":
			if r.Duration <= 0 {
				continue
			}
			e = engine.Event{Kind: engine.Duration, Duration: secs(r.Duration)}
		case "paused":
			d.mu.Lock()
			if !r.Paused && !d.playing {
				d.baseAt = time.Now() // resumes from where it stopped
			} else if r.Paused && d.playing {
				d.base += time.Since(d.baseAt).Seconds()
				d.baseAt = time.Now()
			}
			d.playing = !r.Paused
			d.mu.Unlock()
			e = engine.Event{Kind: engine.Paused, Paused: r.Paused}
		case "ended":
			e = engine.Event{Kind: engine.Ended, Path: path, Reason: engine.EOF, Continues: r.Continues}
			if !r.Continues {
				d.setClock(0, ptr(false))
			}
			if r.Reason == "error" {
				e.Reason, e.Err = engine.Error, errString(orDefault(r.Error, "the browser can't play this song"))
			}
		default:
			continue
		}
		if e.Kind == engine.Position {
			select {
			case d.events <- e:
			default:
			}
			continue
		}
		select {
		case d.events <- e:
		case <-time.After(2 * time.Second):
		}
	}
}

func secs(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

type errString string

func (e errString) Error() string { return string(e) }

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

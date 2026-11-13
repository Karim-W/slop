package engine

import (
	"errors"
	"sync"
	"time"
)

// Router is an Engine that plays through one of several outputs: this
// machine's mpv, or a browser that kyro's web page turned into a speaker.
// Switching hands the song over at the same position, paused or not, so
// the player above never notices which output plays.
//
// Media keys (Remote) from any output reach the player, so the keyboard
// still drives a song playing on a TV. Everything else an inactive output
// reports is dropped.
type Router struct {
	mu     sync.Mutex
	outs   map[string]*output
	order  []string
	def    string // the local output, never removed
	active string
	events chan Event

	// What the player last asked for, replayed when the output changes.
	path, next string
	loaded     bool
	paused     bool
	pos        time.Duration
	volume     int
	filter     string
	// swallow is the path whose Started a hand-over causes: the song
	// continues, it doesn't start again.
	swallow string
}

type output struct {
	Output
	eng  Engine
	stop chan struct{}
}

// Output describes an output, for pickers.
type Output struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"` // "local" or "browser"
	Active bool   `json:"active"`
}

// Starter is an engine that can start a file part-way through, paused or
// not, which makes a hand-over seamless.
type Starter interface {
	PlayFrom(path string, at time.Duration, paused bool) error
}

// ErrNoOutput means the output asked for isn't there (any more).
var ErrNoOutput = errors.New("that output is gone")

// NewRouter starts with the local output (eng may be nil when mpv is
// missing; it then fails to play, as before).
func NewRouter(id, name string, eng Engine, volume int, unavailable error) *Router {
	if eng == nil {
		eng = Unavailable(unavailable)
	}
	r := &Router{outs: map[string]*output{}, events: make(chan Event, 64), volume: volume, def: id, active: id}
	r.add(id, name, "local", eng)
	return r
}

// Add registers an output (a browser speaker). Adding an existing ID
// replaces its engine.
func (r *Router) Add(id, name string, eng Engine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if o, ok := r.outs[id]; ok {
		if o.eng == eng {
			o.Name = name
			return
		}
		r.drop(id)
	}
	r.add(id, name, "browser", eng)
}

func (r *Router) add(id, name, kind string, eng Engine) {
	o := &output{Output: Output{ID: id, Name: name, Kind: kind}, eng: eng, stop: make(chan struct{})}
	r.outs[id] = o
	r.order = append(r.order, id)
	go r.forward(o)
}

// Remove unregisters an output. When it was playing, the song moves to
// the local output, paused where it was: nothing starts blaring.
func (r *Router) Remove(id string) {
	r.mu.Lock()
	if id == r.def || r.outs[id] == nil {
		r.mu.Unlock()
		return
	}
	wasActive := r.active == id
	if wasActive {
		r.paused = true
		r.handover(r.def)
	}
	r.drop(id)
	r.mu.Unlock()
	if wasActive {
		// Off the caller's goroutine: the UI may be the one removing, and
		// it is also the one reading events.
		go r.send(Event{Kind: Paused, Paused: true})
	}
}

func (r *Router) drop(id string) {
	o := r.outs[id]
	close(o.stop)
	delete(r.outs, id)
	for i, x := range r.order {
		if x == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	if id != r.active {
		o.eng.Stop()
	}
}

// Switch moves playback to output id.
func (r *Router) Switch(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.outs[id] == nil {
		return ErrNoOutput
	}
	if id == r.active {
		return nil
	}
	return r.handover(id)
}

// handover starts the current song on id where it is and silences the old
// output. r.mu is held.
func (r *Router) handover(id string) error {
	old, now := r.outs[r.active], r.outs[id]
	r.active = id
	now.eng.SetVolume(r.volume)
	now.eng.SetFilter(r.filter)
	var err error
	if r.loaded {
		r.swallow = r.path
		if s, ok := now.eng.(Starter); ok {
			err = s.PlayFrom(r.path, r.pos, r.paused)
		} else if err = now.eng.Play(r.path); err == nil {
			now.eng.Seek(r.pos)
			now.eng.SetPaused(r.paused)
		}
		if err == nil && r.next != "" {
			now.eng.Next(r.next)
		}
	}
	if old != nil {
		old.eng.Stop()
	}
	return err
}

// Active is the ID of the output playing now.
func (r *Router) Active() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// Outputs lists every output, the local one first.
func (r *Router) Outputs() []Output {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Output, 0, len(r.order))
	for _, id := range r.order {
		o := r.outs[id].Output
		o.Active = id == r.active
		out = append(out, o)
	}
	return out
}

// forward relays one output's events until it is removed.
func (r *Router) forward(o *output) {
	ch := o.eng.Events()
	for {
		select {
		case <-o.stop:
			return
		case e := <-ch:
			if r.accept(o.ID, e) {
				r.send(e)
			}
		}
	}
}

// accept records what an event says about playback and reports whether
// the player should hear it.
func (r *Router) accept(id string, e Event) bool {
	if e.Kind == Remote {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if id != r.active {
		return false
	}
	switch e.Kind {
	case Started:
		r.path, r.loaded = e.Path, true
		if e.Path == r.next {
			r.next = ""
		}
		if r.swallow != "" && e.Path == r.swallow {
			r.swallow = ""
			return false
		}
	case Position:
		r.pos = e.Pos
	case Paused:
		r.paused = e.Paused
	case Ended:
		if !e.Continues && (e.Reason == EOF || e.Reason == Error) && (e.Path == "" || e.Path == r.path) {
			r.loaded = false
		}
	}
	return true
}

func (r *Router) send(e Event) {
	if e.Kind == Position {
		select {
		case r.events <- e:
		default:
		}
		return
	}
	r.events <- e
}

func (r *Router) cur() Engine { return r.outs[r.active].eng }

func (r *Router) Play(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.path, r.next, r.loaded, r.paused, r.pos, r.swallow = path, "", true, false, 0, ""
	return r.cur().Play(path)
}

func (r *Router) Next(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next = path
	return r.cur().Next(path)
}

func (r *Router) SetPaused(p bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cur().SetPaused(p)
}

func (r *Router) Seek(d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pos = d
	return r.cur().Seek(d)
}

func (r *Router) SetVolume(v int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.volume = min(max(v, 0), 100)
	return r.cur().SetVolume(r.volume)
}

func (r *Router) SetFilter(af string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.filter = af
	return r.cur().SetFilter(af)
}

func (r *Router) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded, r.next, r.pos, r.swallow = false, "", 0, ""
	return r.cur().Stop()
}

// Events delivers the active output's events, and media keys from all.
func (r *Router) Events() <-chan Event { return r.events }

// Close closes every output.
func (r *Router) Close() error {
	r.mu.Lock()
	outs := make([]*output, 0, len(r.outs))
	for _, o := range r.outs {
		outs = append(outs, o)
	}
	r.mu.Unlock()
	var first error
	for _, o := range outs {
		if err := o.eng.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Unavailable is an engine that can't play, for when mpv is missing.
func Unavailable(err error) Engine {
	if err == nil {
		err = errors.New("can't play here")
	}
	return unavailable{err}
}

type unavailable struct{ err error }

func (u unavailable) Play(string) error        { return u.err }
func (u unavailable) Next(string) error        { return nil }
func (u unavailable) SetPaused(bool) error     { return u.err }
func (u unavailable) Seek(time.Duration) error { return nil }
func (u unavailable) SetVolume(int) error      { return nil }
func (u unavailable) SetFilter(string) error   { return nil }
func (u unavailable) Stop() error              { return nil }
func (u unavailable) Events() <-chan Event     { return nil }
func (u unavailable) Close() error             { return nil }

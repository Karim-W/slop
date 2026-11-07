// Package player is kyro's playback state: the queue, shuffle and repeat,
// and what is playing right now. It drives an engine.Engine and turns its
// events into state changes that the UI shows and hooks announce.
package player

import (
	"math/rand/v2"
	"slices"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/engine"
	"github.com/karim-w/slop/apps/kyro/internal/library"
)

// Repeat is the repeat mode.
type Repeat string

const (
	RepeatOff Repeat = "off"
	RepeatAll Repeat = "all"
	RepeatOne Repeat = "one"
)

// Next cycles off → all → one → off.
func (r Repeat) Next() Repeat {
	switch r {
	case RepeatOff:
		return RepeatAll
	case RepeatAll:
		return RepeatOne
	}
	return RepeatOff
}

// State is a snapshot of playback.
type State struct {
	Track    library.Track
	Active   bool // a track is loaded (playing or paused)
	Paused   bool
	Pos      time.Duration
	Duration time.Duration
	Volume   int
	Shuffle  bool
	Repeat   Repeat
	// Source is the decoded stream, Output what the device receives.
	Source, Output engine.AudioParams
	Index          int // position in Queue, -1 if none
	Queue          []library.Track
}

// Change is a moment hooks care about.
type Change string

const (
	TrackStarted Change = "track"
	PausedChange Change = "pause"
	Resumed      Change = "resume"
	Stopped      Change = "stop"
)

// Player owns the queue and the engine.
type Player struct {
	eng    engine.Engine
	queue  []library.Track // play order
	order  []library.Track // the order before shuffling
	idx    int
	st     State
	random *rand.Rand
}

// New wraps an engine.
func New(eng engine.Engine, volume int, shuffle bool, repeat Repeat) *Player {
	if repeat == "" {
		repeat = RepeatOff
	}
	return &Player{eng: eng, idx: -1, random: rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7)),
		st: State{Volume: volume, Shuffle: shuffle, Repeat: repeat, Index: -1}}
}

// State returns the current snapshot.
func (p *Player) State() State {
	s := p.st
	s.Queue = p.queue
	s.Index = p.idx
	return s
}

// PlayList replaces the queue with tracks and plays tracks[start].
func (p *Player) PlayList(tracks []library.Track, start int) error {
	if len(tracks) == 0 || start < 0 || start >= len(tracks) {
		return nil
	}
	tracks = playable(tracks, &start)
	if len(tracks) == 0 {
		return nil
	}
	p.order = slices.Clone(tracks)
	p.queue = slices.Clone(tracks)
	p.idx = start
	if p.st.Shuffle {
		p.shuffleFrom(start)
	}
	return p.load()
}

// playable drops missing files, keeping start on the same track.
func playable(ts []library.Track, start *int) []library.Track {
	out := make([]library.Track, 0, len(ts))
	ns := 0
	for i, t := range ts {
		if t.Missing && i != *start {
			continue
		}
		if i == *start {
			ns = len(out)
		}
		out = append(out, t)
	}
	*start = ns
	return out
}

// Enqueue adds tracks to the end of the queue.
func (p *Player) Enqueue(tracks ...library.Track) {
	p.queue = append(p.queue, tracks...)
	p.order = append(p.order, tracks...)
	if p.st.Active {
		p.queueNext()
	}
}

// PlayNext inserts a track right after the current one.
func (p *Player) PlayNext(t library.Track) {
	at := p.idx + 1
	if at > len(p.queue) {
		at = len(p.queue)
	}
	p.queue = slices.Insert(p.queue, at, t)
	p.order = append(p.order, t)
	if p.st.Active {
		p.queueNext()
	}
}

// load starts the track at idx.
func (p *Player) load() error {
	t := p.queue[p.idx]
	p.st.Track, p.st.Active, p.st.Paused = t, true, false
	p.st.Pos, p.st.Duration = 0, t.Duration
	p.st.Source, p.st.Output = engine.AudioParams{}, engine.AudioParams{}
	if err := p.eng.Play(t.Path); err != nil {
		return err
	}
	return p.queueNext()
}

// nextIndex is the track after idx under the repeat mode.
func (p *Player) nextIndex() (int, bool) {
	if p.idx < 0 || len(p.queue) == 0 {
		return 0, false
	}
	switch {
	case p.st.Repeat == RepeatOne:
		return p.idx, true
	case p.idx+1 < len(p.queue):
		return p.idx + 1, true
	case p.st.Repeat == RepeatAll:
		return 0, true
	}
	return 0, false
}

// queueNext tells the engine what follows, for a gapless transition. The
// same file again (repeat one, a duplicate) restarts on end instead.
func (p *Player) queueNext() error {
	if i, ok := p.nextIndex(); ok && p.queue[i].Path != p.st.Track.Path {
		return p.eng.Next(p.queue[i].Path)
	}
	return p.eng.Next("")
}

// Toggle pauses or resumes; with nothing loaded it starts the queue.
func (p *Player) Toggle() error {
	if !p.st.Active {
		if len(p.queue) > 0 {
			p.idx = max(p.idx, 0)
			return p.load()
		}
		return nil
	}
	return p.eng.SetPaused(!p.st.Paused)
}

// SetPaused pauses or resumes.
func (p *Player) SetPaused(v bool) error {
	if !p.st.Active {
		return nil
	}
	return p.eng.SetPaused(v)
}

// Next skips to the next track.
func (p *Player) Next() error {
	i, ok := p.nextIndex()
	if p.st.Repeat == RepeatOne { // an explicit skip leaves the loop
		i, ok = p.idx+1, p.idx+1 < len(p.queue)
		if !ok && len(p.queue) > 0 {
			i, ok = 0, true
		}
	}
	if !ok {
		return p.Stop()
	}
	p.idx = i
	return p.load()
}

// Prev restarts the track, or goes back one if it just started.
func (p *Player) Prev() error {
	if !p.st.Active {
		return nil
	}
	if p.st.Pos > 3*time.Second || p.idx == 0 && p.st.Repeat != RepeatAll {
		return p.eng.Seek(0)
	}
	p.idx--
	if p.idx < 0 {
		p.idx = len(p.queue) - 1
	}
	return p.load()
}

// Seek jumps to an absolute position.
func (p *Player) Seek(d time.Duration) error {
	if !p.st.Active {
		return nil
	}
	d = max(0, d)
	if p.st.Duration > 0 {
		d = min(d, p.st.Duration-time.Second)
	}
	p.st.Pos = d
	return p.eng.Seek(d)
}

// SeekBy moves relative to the current position.
func (p *Player) SeekBy(d time.Duration) error { return p.Seek(p.st.Pos + d) }

// SetVolume sets 0–100.
func (p *Player) SetVolume(v int) error {
	p.st.Volume = min(max(v, 0), 100)
	return p.eng.SetVolume(p.st.Volume)
}

// SetFilter sets the engine's audio filters (a sound mode).
func (p *Player) SetFilter(af string) error { return p.eng.SetFilter(af) }

// Stop ends playback.
func (p *Player) Stop() error {
	p.st.Active, p.st.Paused, p.st.Pos = false, false, 0
	return p.eng.Stop()
}

// ToggleShuffle turns shuffle on (randomising what comes after the current
// track) or off (back to the original order).
func (p *Player) ToggleShuffle() {
	p.st.Shuffle = !p.st.Shuffle
	if len(p.queue) == 0 {
		return
	}
	if p.st.Shuffle {
		p.shuffleFrom(p.idx)
	} else {
		cur := p.queue[max(p.idx, 0)]
		p.queue = slices.Clone(p.order)
		p.idx = max(slices.IndexFunc(p.queue, func(t library.Track) bool { return t.ID == cur.ID }), 0)
	}
	if p.st.Active {
		p.queueNext()
	}
}

// shuffleFrom puts queue[i] first and shuffles the rest.
func (p *Player) shuffleFrom(i int) {
	if i < 0 || i >= len(p.queue) {
		i = 0
	}
	cur := p.queue[i]
	rest := slices.Delete(slices.Clone(p.queue), i, i+1)
	p.random.Shuffle(len(rest), func(a, b int) { rest[a], rest[b] = rest[b], rest[a] })
	p.queue = append([]library.Track{cur}, rest...)
	p.idx = 0
}

// CycleRepeat moves to the next repeat mode.
func (p *Player) CycleRepeat() {
	p.st.Repeat = p.st.Repeat.Next()
	if p.st.Active {
		p.queueNext()
	}
}

// Handle applies an engine event and reports the change hooks should hear
// about ("" if none) and any error to show.
func (p *Player) Handle(e engine.Event) (Change, error) {
	switch e.Kind {
	case engine.Started:
		if !p.st.Active {
			return "", nil
		}
		if e.Path != p.st.Track.Path {
			// A gapless transition: the queued next track began.
			i, ok := p.nextIndex()
			if !ok || p.queue[i].Path != e.Path {
				return "", nil
			}
			p.idx = i
			t := p.queue[i]
			p.st.Track, p.st.Pos, p.st.Duration = t, 0, t.Duration
			p.st.Source, p.st.Output = engine.AudioParams{}, engine.AudioParams{}
			p.queueNext()
		}
		return TrackStarted, nil
	case engine.Position:
		if p.st.Active {
			p.st.Pos = e.Pos
		}
	case engine.Duration:
		if p.st.Active {
			p.st.Duration = e.Duration
			if p.st.Track.Duration == 0 {
				p.st.Track.Duration = e.Duration
			}
		}
	case engine.Paused:
		if !p.st.Active || p.st.Paused == e.Paused {
			p.st.Paused = e.Paused
			return "", nil
		}
		p.st.Paused = e.Paused
		if e.Paused {
			return PausedChange, nil
		}
		return Resumed, nil
	case engine.Params:
		if e.Source.SampleRate > 0 {
			p.st.Source = e.Source
		}
		if e.Output.SampleRate > 0 {
			p.st.Output = e.Output
		}
	case engine.Ended:
		if !p.st.Active || e.Continues || e.Path != "" && e.Path != p.st.Track.Path {
			return "", nil
		}
		switch e.Reason {
		case engine.EOF:
			if i, ok := p.nextIndex(); ok {
				p.idx = i
				return "", p.load() // Started follows and reports the track
			}
			p.st.Active, p.st.Pos = false, 0
			return Stopped, nil
		case engine.Error:
			err := e.Err
			if i, ok := p.nextIndex(); ok && i != p.idx {
				p.idx = i
				if lerr := p.load(); lerr != nil {
					return "", lerr
				}
				return "", err
			}
			p.st.Active = false
			return Stopped, err
		}
	case engine.Exited:
		was := p.st.Active
		p.st.Active = false
		if was {
			return Stopped, e.Err
		}
		return "", e.Err
	}
	return "", nil
}

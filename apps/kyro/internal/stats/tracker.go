package stats

import "time"

// Tracker turns playback into Plays. Feed it what the player does; it
// measures listening time from position updates, so pauses and the parts
// you seek over don't count.
type Tracker struct {
	cur  *Play
	last time.Duration // last position seen
	eof  bool          // the current song reached its end
	Now  func() time.Time
}

// NewTracker starts with nothing playing.
func NewTracker() *Tracker { return &Tracker{Now: time.Now} }

// Current is the play in progress, or nil.
func (t *Tracker) Current() *Play { return t.cur }

// Begin starts a play of p, returning the previous one, finished.
func (t *Tracker) Begin(p Play) *Play {
	prev := t.Finish(Skipped)
	p.Start = t.Now()
	t.cur, t.last, t.eof = &p, 0, false
	return prev
}

// Position records the playback position.
func (t *Tracker) Position(pos time.Duration) {
	if t.cur == nil {
		return
	}
	d := pos - t.last
	switch {
	case d > 0 && d <= 3*time.Second: // normal progress
		t.cur.Listened += d
	case d > 3*time.Second || d < -time.Second: // a jump: a seek
		t.cur.Seeks++
	}
	t.last = pos
}

// Paused counts a pause.
func (t *Tracker) Paused() {
	if t.cur != nil {
		t.cur.Pauses++
	}
}

// EOF marks that the song played to its end.
func (t *Tracker) EOF() { t.eof = true }

// Finish ends the current play and returns it (nil if none). reason
// applies unless the song reached its end, which is always Finished.
func (t *Tracker) Finish(reason string) *Play {
	p := t.cur
	if p == nil {
		return nil
	}
	t.cur = nil
	p.End, p.EndPos = t.Now(), t.last
	near := p.Duration > 0 && t.last >= p.Duration-5*time.Second
	if t.eof || near && reason != Failed {
		reason = Finished
	}
	p.EndReason = reason
	return p
}

package player

import (
	"fmt"
	"testing"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/engine"
	"github.com/karim-w/slop/apps/kyro/internal/library"
)

// fake records engine calls and lets tests play its events.
type fake struct {
	playing, next string
	paused        bool
	seek          time.Duration
	volume        int
	stopped       bool
}

func (f *fake) Play(p string) error         { f.playing, f.stopped = p, false; return nil }
func (f *fake) Next(p string) error         { f.next = p; return nil }
func (f *fake) SetPaused(v bool) error      { f.paused = v; return nil }
func (f *fake) Seek(d time.Duration) error  { f.seek = d; return nil }
func (f *fake) SetVolume(v int) error       { f.volume = v; return nil }
func (f *fake) SetFilter(string) error      { return nil }
func (f *fake) Stop() error                 { f.stopped = true; return nil }
func (f *fake) Events() <-chan engine.Event { return nil }
func (f *fake) Close() error                { return nil }

func tracks(n int) []library.Track {
	out := make([]library.Track, n)
	for i := range out {
		p := fmt.Sprintf("/m/%d.flac", i)
		out[i] = library.Track{ID: library.IDFor(p), Path: p, Title: fmt.Sprint(i), Duration: time.Minute}
	}
	return out
}

func TestGaplessAdvanceQueuesTheFollowingTrack(t *testing.T) {
	f := &fake{}
	p := New(f, 50, false, RepeatOff)
	ts := tracks(3)
	p.PlayList(ts, 0)
	if f.playing != ts[0].Path || f.next != ts[1].Path {
		t.Fatalf("play %s next %s", f.playing, f.next)
	}
	if c, _ := p.Handle(engine.Event{Kind: engine.Started, Path: ts[0].Path}); c != TrackStarted {
		t.Fatalf("change %q", c)
	}
	// mpv: end of 0 (a follow-up is queued), then 1 starts.
	if c, _ := p.Handle(engine.Event{Kind: engine.Ended, Path: ts[0].Path, Reason: engine.EOF, Continues: true}); c != "" {
		t.Fatalf("continuing end should be silent, got %q", c)
	}
	c, _ := p.Handle(engine.Event{Kind: engine.Started, Path: ts[1].Path})
	if c != TrackStarted || p.State().Track.ID != ts[1].ID || f.next != ts[2].Path {
		t.Fatalf("change %q track %s next %s", c, p.State().Track.Title, f.next)
	}
	p.Handle(engine.Event{Kind: engine.Started, Path: ts[2].Path})
	if f.next != "" {
		t.Fatalf("nothing should follow the last track, got %s", f.next)
	}
	if c, _ := p.Handle(engine.Event{Kind: engine.Ended, Path: ts[2].Path, Reason: engine.EOF}); c != Stopped || p.State().Active {
		t.Fatalf("end of queue: %q active=%v", c, p.State().Active)
	}
}

func TestRepeatModes(t *testing.T) {
	f := &fake{}
	p := New(f, 50, false, RepeatAll)
	ts := tracks(2)
	p.PlayList(ts, 1)
	if f.next != ts[0].Path {
		t.Fatalf("repeat all should wrap, next=%s", f.next)
	}
	p.CycleRepeat() // one
	if f.next != "" {
		t.Fatalf("repeat one restarts on end instead of queueing, next=%s", f.next)
	}
	p.Handle(engine.Event{Kind: engine.Ended, Path: ts[1].Path, Reason: engine.EOF})
	if f.playing != ts[1].Path || !p.State().Active {
		t.Fatal("repeat one should replay the same track")
	}
	p.Next() // an explicit skip leaves the loop
	if p.State().Track.ID != ts[0].ID {
		t.Fatalf("next under repeat one moved to %s", p.State().Track.Title)
	}
}

func TestShuffleKeepsCurrentAndRestoresOrder(t *testing.T) {
	f := &fake{}
	p := New(f, 50, false, RepeatOff)
	ts := tracks(20)
	p.PlayList(ts, 5)
	p.ToggleShuffle()
	s := p.State()
	if s.Queue[s.Index].ID != ts[5].ID || len(s.Queue) != 20 {
		t.Fatal("shuffle must keep the current track playing")
	}
	p.ToggleShuffle()
	s = p.State()
	if s.Index != 5 || s.Queue[6].ID != ts[6].ID {
		t.Fatalf("unshuffle should restore order at the same track, idx=%d", s.Index)
	}
}

func TestPrevRestartsThenGoesBack(t *testing.T) {
	f := &fake{}
	p := New(f, 50, false, RepeatOff)
	ts := tracks(3)
	p.PlayList(ts, 1)
	p.Handle(engine.Event{Kind: engine.Position, Pos: 10 * time.Second})
	p.Prev()
	if f.playing != ts[1].Path || f.seek != 0 {
		t.Fatal("prev after 3s should restart the track")
	}
	p.Handle(engine.Event{Kind: engine.Position, Pos: time.Second})
	p.Prev()
	if p.State().Track.ID != ts[0].ID {
		t.Fatal("prev near the start should go back")
	}
}

func TestBrokenFileSkipsAhead(t *testing.T) {
	f := &fake{}
	p := New(f, 50, false, RepeatOff)
	ts := tracks(2)
	p.PlayList(ts, 0)
	_, err := p.Handle(engine.Event{Kind: engine.Ended, Path: ts[0].Path, Reason: engine.Error, Err: fmt.Errorf("bad")})
	if err == nil || f.playing != ts[1].Path {
		t.Fatalf("err=%v playing=%s", err, f.playing)
	}
}

func TestPauseChangesAndVolumeClamp(t *testing.T) {
	f := &fake{}
	p := New(f, 50, false, RepeatOff)
	p.PlayList(tracks(1), 0)
	if c, _ := p.Handle(engine.Event{Kind: engine.Paused, Paused: true}); c != PausedChange {
		t.Fatal(c)
	}
	if c, _ := p.Handle(engine.Event{Kind: engine.Paused, Paused: false}); c != Resumed {
		t.Fatal(c)
	}
	p.SetVolume(400)
	if f.volume != 100 {
		t.Fatalf("volume %d", f.volume)
	}
}

func TestMissingFilesAreSkippedWhenQueueing(t *testing.T) {
	f := &fake{}
	p := New(f, 50, false, RepeatOff)
	ts := tracks(3)
	ts[1].Missing = true
	p.PlayList(ts, 0)
	if f.next != ts[2].Path {
		t.Fatalf("missing file queued: %s", f.next)
	}
}

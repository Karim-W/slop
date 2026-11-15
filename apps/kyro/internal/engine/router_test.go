package engine

import (
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// fakeOut records the calls an output gets and lets a test send events.
type fakeOut struct {
	mu     sync.Mutex
	calls  []string
	events chan Event
}

func newFakeOut() *fakeOut { return &fakeOut{events: make(chan Event, 16)} }

func (f *fakeOut) log(s string) error {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
	return nil
}

func (f *fakeOut) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeOut) Play(p string) error { return f.log("play " + p) }
func (f *fakeOut) PlayFrom(p string, at time.Duration, paused bool) error {
	return f.log("from " + p + " " + at.String() + map[bool]string{true: " paused", false: ""}[paused])
}
func (f *fakeOut) Next(p string) error { return f.log("next " + p) }
func (f *fakeOut) SetPaused(p bool) error {
	return f.log(map[bool]string{true: "pause", false: "resume"}[p])
}
func (f *fakeOut) Seek(d time.Duration) error { return f.log("seek " + d.String()) }
func (f *fakeOut) SetVolume(v int) error      { return nil }
func (f *fakeOut) SetFilter(string) error     { return nil }
func (f *fakeOut) Stop() error                { return f.log("stop") }
func (f *fakeOut) Events() <-chan Event       { return f.events }
func (f *fakeOut) Close() error               { return nil }

func next(t *testing.T, r *Router) Event {
	t.Helper()
	select {
	case e := <-r.Events():
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
	return Event{}
}

func quiet(t *testing.T, r *Router) {
	t.Helper()
	select {
	case e := <-r.Events():
		t.Fatalf("unexpected event %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRouterHandsTheSongOver(t *testing.T) {
	mac, tv := newFakeOut(), newFakeOut()
	r := NewRouter("local", "Mac", mac, 60, nil)
	r.Add("tv", "TV", tv)

	r.Play("/a.flac")
	r.Next("/b.flac")
	mac.events <- Event{Kind: Started, Path: "/a.flac"}
	if e := next(t, r); e.Kind != Started {
		t.Fatalf("got %+v", e)
	}
	mac.events <- Event{Kind: Position, Pos: 42 * time.Second}
	next(t, r)

	if err := r.Switch("tv"); err != nil {
		t.Fatal(err)
	}
	if got := tv.Calls(); !slices.Equal(got, []string{"from /a.flac 42s", "next /b.flac"}) {
		t.Fatalf("tv got %v", got)
	}
	if got := mac.Calls(); got[len(got)-1] != "stop" {
		t.Fatalf("mac should stop, got %v", got)
	}
	// The hand-over's own Started is not a new play; the old output's
	// leftovers are not heard either.
	tv.events <- Event{Kind: Started, Path: "/a.flac"}
	mac.events <- Event{Kind: Ended, Path: "/a.flac", Reason: Stopped}
	quiet(t, r)
	tv.events <- Event{Kind: Position, Pos: 43 * time.Second}
	if e := next(t, r); e.Pos != 43*time.Second {
		t.Fatalf("got %+v", e)
	}
	// Media keys on the Mac still reach the player.
	mac.events <- Event{Kind: Remote, Command: RemoteNext}
	if e := next(t, r); e.Kind != Remote {
		t.Fatalf("got %+v", e)
	}
	if o := r.Outputs(); len(o) != 2 || o[0].Active || !o[1].Active || o[1].Kind != "browser" {
		t.Fatalf("outputs %+v", o)
	}
}

func TestRouterFallsBackPausedWhenAnOutputLeaves(t *testing.T) {
	mac, phone := newFakeOut(), newFakeOut()
	r := NewRouter("local", "Mac", mac, 60, nil)
	r.Add("phone", "Phone", phone)
	r.Switch("phone")
	r.Play("/a.flac")
	phone.events <- Event{Kind: Position, Pos: 10 * time.Second}
	next(t, r)

	r.Remove("phone")
	if got := mac.Calls(); !slices.Contains(got, "from /a.flac 10s paused") {
		t.Fatalf("mac got %v", got)
	}
	if e := next(t, r); e.Kind != Paused || !e.Paused {
		t.Fatalf("want a pause, got %+v", e)
	}
	if r.Active() != "local" || len(r.Outputs()) != 1 {
		t.Fatalf("active %q, outputs %+v", r.Active(), r.Outputs())
	}
	if err := r.Switch("phone"); err != ErrNoOutput {
		t.Fatalf("switch to a gone output: %v", err)
	}
	r.Remove("local") // the local output stays
	if len(r.Outputs()) != 1 {
		t.Fatal("local output removed")
	}
}

func TestMpvPlayFromStartsPartWayPaused(t *testing.T) {
	m := startTestMpv(t)
	p := filepath.Join(t.TempDir(), "a.wav")
	writeWAV(t, p, 44100, 2, 20*time.Second)
	if err := m.PlayFrom(p, 12*time.Second, true); err != nil {
		t.Fatal(err)
	}
	var seen []Event
	e := wait(t, m, &seen, func(e Event) bool { return e.Kind == Position && e.Pos > 0 })
	if e.Pos < 11900*time.Millisecond || e.Pos > 12500*time.Millisecond {
		t.Fatalf("started at %v, want 12s", e.Pos)
	}
	time.Sleep(400 * time.Millisecond)
	for {
		select {
		case e := <-m.Events():
			if e.Kind == Position && e.Pos > 12500*time.Millisecond {
				t.Fatalf("still playing at %v; want paused", e.Pos)
			}
			continue
		default:
		}
		break
	}
}

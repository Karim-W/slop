package ipc

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/hooks"
)

// sockPath keeps Unix socket paths short (macOS caps them near 104 bytes).
func sockPath(t *testing.T) string {
	d, err := os.MkdirTemp("/tmp", "kyro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return filepath.Join(d, "s")
}

func TestCommandsAndSubscribe(t *testing.T) {
	path := sockPath(t)
	var got []Request
	srv, err := Listen(path, func(r Request) Response {
		got = append(got, r)
		return Response{OK: true, State: &hooks.Event{Event: "track", Track: &hooks.Track{Title: "Origami"}}}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	resp, err := Call(path, Request{Cmd: "volume", Arg: "+5"})
	if err != nil || !resp.OK || resp.State.Track.Title != "Origami" {
		t.Fatalf("%+v %v", resp, err)
	}
	if got[0].Cmd != "volume" || got[0].Arg != "+5" {
		t.Fatalf("%+v", got)
	}

	events := make(chan hooks.Event, 4)
	go Subscribe(path, func(e hooks.Event) bool { events <- e; return true })
	if e := <-events; e.Event != "track" {
		t.Fatalf("first event should be the current state, got %+v", e)
	}
	time.Sleep(50 * time.Millisecond) // let the subscription register
	srv.Broadcast(hooks.Event{Event: "pause"})
	select {
	case e := <-events:
		if e.Event != "pause" {
			t.Fatalf("%+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast")
	}
}

func TestSecondInstanceAndStaleSocket(t *testing.T) {
	path := sockPath(t)
	srv, err := Listen(path, func(Request) Response { return Response{OK: true} })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path, nil); err != ErrRunning {
		t.Fatalf("second listen: %v", err)
	}
	srv.Close()
	os.WriteFile(path, nil, 0o600) // a crash leaves a dead file behind
	srv2, err := Listen(path, func(Request) Response { return Response{OK: true} })
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	srv2.Close()
	if _, err := Call(path, Request{Cmd: "status"}); err == nil {
		t.Fatal("call to a stopped kyro should fail")
	}
}

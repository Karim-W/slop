package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/home"
)

func TestHooksReceiveTheEvent(t *testing.T) {
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, "hooks")
	os.MkdirAll(hooksDir, 0o755)
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o755)
	// executable named after the event: gets JSON on stdin
	os.WriteFile(filepath.Join(hooksDir, "track"), []byte("#!/bin/sh\ncat > "+out+"/stdin.json\n"), 0o755)
	// catch-all hook
	os.WriteFile(filepath.Join(hooksDir, "event"), []byte("#!/bin/sh\necho \"$KYRO_EVENT $KYRO_TITLE\" >> "+out+"/all.txt\n"), 0o755)
	// not executable: ignored
	os.WriteFile(filepath.Join(hooksDir, "pause"), []byte("#!/bin/sh\ntouch "+out+"/nope\n"), 0o644)

	var published []Event
	d := &Dispatcher{
		Dir:       hooksDir,
		StatePath: filepath.Join(dir, "now-playing.json"),
		Commands:  []home.Hook{{Event: "track", Command: "printf '%s' \"$KYRO_ARTIST\" > " + out + "/cmd.txt"}},
		Publish:   func(e Event) { published = append(published, e) },
	}
	d.Fire(Event{Event: "track", Playing: true, Track: &Track{Title: "Solar Wind", Artist: "Nebula Drift", Duration: 20}})
	d.Fire(Event{Event: "pause"})
	d.Wait()

	var got Event
	b, _ := os.ReadFile(filepath.Join(out, "stdin.json"))
	if json.Unmarshal(b, &got) != nil || got.Track == nil || got.Track.Title != "Solar Wind" {
		t.Fatalf("stdin payload %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "cmd.txt")); string(b) != "Nebula Drift" {
		t.Fatalf("config hook env: %q", b)
	}
	all, _ := os.ReadFile(filepath.Join(out, "all.txt"))
	if !strings.Contains(string(all), "track Solar Wind") || !strings.Contains(string(all), "pause") {
		t.Fatalf("catch-all hook saw %q", all)
	}
	if _, err := os.Stat(filepath.Join(out, "nope")); err == nil {
		t.Fatal("a non-executable hook ran")
	}
	var state Event
	b, _ = os.ReadFile(d.StatePath)
	if json.Unmarshal(b, &state) != nil || state.Event != "pause" {
		t.Fatalf("now-playing.json = %s", b)
	}
	if len(published) != 2 {
		t.Fatalf("published %d", len(published))
	}
}

func TestSlowHookIsReplacedNotQueued(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "count")
	d := &Dispatcher{Commands: []home.Hook{{Event: "track", Command: "sleep 5; echo x >> " + marker}}}
	start := time.Now()
	for i := 0; i < 5; i++ {
		d.Fire(Event{Event: "track"}) // fast skipping
	}
	if time.Since(start) > time.Second {
		t.Fatal("Fire blocked on a hook")
	}
	// only the last run survives; cancel the rest and don't wait 5s for it
	d.mu.Lock()
	n := len(d.running)
	for _, r := range d.running {
		r.cancel()
	}
	d.mu.Unlock()
	d.Wait()
	if n != 1 {
		t.Fatalf("%d runs still active, want 1", n)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("cancelled hooks finished anyway")
	}
}

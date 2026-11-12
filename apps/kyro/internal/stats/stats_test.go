package stats

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newKey(t *testing.T) []byte {
	k := make([]byte, 32)
	rand.Read(k)
	return k
}

func play(title string, listened time.Duration, reason string, at time.Time) Play {
	return Play{Start: at, End: at.Add(listened), TrackID: "id-" + title, Path: "/music/" + title + ".flac",
		Title: title, Artist: "Nebula Drift", Album: "Afterglow", Duration: 4 * time.Minute,
		Listened: listened, EndReason: reason, Context: "albums", ContextName: "Afterglow"}
}

func TestPlaysPersistAndSummarize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats", "kyro.db")
	key := newKey(t)
	s, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	jan := time.Date(2026, 1, 10, 22, 0, 0, 0, time.Local)
	s.AddPlay(play("Solar Wind", 4*time.Minute, Finished, jan))
	s.AddPlay(play("Solar Wind", 3*time.Minute, Skipped, jan.Add(time.Hour)))
	s.AddPlay(play("Event Horizon", 10*time.Second, Skipped, jan.Add(2*time.Hour))) // a skip: not a play
	s.AddEvent(Event{At: jan, Kind: "love", TrackID: "id-Solar Wind", Title: "Solar Wind"})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(path, key) // reopen: still there
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sum, err := s.Summarize(time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local), time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Plays != 2 || sum.AllPlays != 3 || sum.Songs != 1 || sum.Listened != 7*time.Minute {
		t.Errorf("summary %+v", sum)
	}
	if sum.TopSong != "Solar Wind — Nebula Drift" || sum.TopSongN != 2 || sum.TopArtist != "Nebula Drift" || sum.EventCount != 1 {
		t.Errorf("summary %+v", sum)
	}
}

func TestDatabaseIsEncryptedAndPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stats")
	path := filepath.Join(dir, "kyro.db")
	s, err := Open(path, newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	s.AddPlay(play("Solar Wind", time.Minute, Finished, time.Now()))
	s.Close()

	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		for _, plain := range []string{"SQLite format 3", "Solar Wind", "Nebula Drift", "CREATE TABLE", "plays"} {
			if bytes.Contains(raw, []byte(plain)) {
				t.Errorf("%s contains %q in the clear", filepath.Base(f), plain)
			}
		}
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("database mode %v, want 0600", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("folder mode %v, want 0700", fi.Mode().Perm())
	}
}

func TestWrongKeyCantRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kyro.db")
	s, err := Open(path, newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	s.AddPlay(play("Solar Wind", time.Minute, Finished, time.Now()))
	s.Close()
	if _, err := Open(path, newKey(t)); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("another key should fail with ErrWrongKey, got %v", err)
	}
}

type memKeyring map[string]string

func (m memKeyring) Get(k string) (string, bool) { v, ok := m[k]; return v, ok }
func (m memKeyring) Set(k, v string) error       { m[k] = v; return nil }

func TestKeyIsCreatedOnceAndNeverReplaced(t *testing.T) {
	kr := memKeyring{}
	path := filepath.Join(t.TempDir(), "kyro.db")
	k1, err := LoadKey(kr, path)
	if err != nil || len(k1) != 32 {
		t.Fatalf("first key: %v", err)
	}
	k2, _ := LoadKey(kr, path)
	if !bytes.Equal(k1, k2) {
		t.Fatal("the key should be reused")
	}
	s, _ := Open(path, k1)
	s.Close()
	delete(kr, KeychainAccount) // the keychain lost it
	if _, err := LoadKey(kr, path); !errors.Is(err, ErrKeyLost) {
		t.Fatalf("with a database and no key, want ErrKeyLost, got %v", err)
	}
	if _, ok := kr[KeychainAccount]; ok {
		t.Fatal("must not make a new key over an existing database")
	}
	kr[KeychainAccount] = strings.Repeat("zz", 32)
	if _, err := LoadKey(kr, path); err == nil {
		t.Fatal("a damaged key should be an error")
	}
}

func TestTrackerMeasuresWhatYouHeard(t *testing.T) {
	now := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	tr := &Tracker{Now: func() time.Time { return now }}
	tr.Begin(Play{Title: "Solar Wind", Duration: 4 * time.Minute})
	for s := 1; s <= 60; s++ { // a minute of normal playback
		tr.Position(time.Duration(s) * time.Second)
	}
	tr.Paused()
	tr.Position(2 * time.Minute) // seek ahead: not heard
	tr.Position(2*time.Minute + time.Second)
	now = now.Add(5 * time.Minute)
	prev := tr.Begin(Play{Title: "Event Horizon", Duration: 3 * time.Minute})
	if prev == nil || prev.EndReason != Skipped || prev.Listened != 61*time.Second || prev.Seeks != 1 || prev.Pauses != 1 {
		t.Fatalf("first play: %+v", prev)
	}
	if prev.EndPos != 2*time.Minute+time.Second || !prev.End.Equal(now) {
		t.Errorf("end: %+v", prev)
	}

	tr.EOF()
	if p := tr.Finish(Stopped); p.EndReason != Finished {
		t.Errorf("a song that reached its end is finished, got %s", p.EndReason)
	}
	if tr.Finish(Quit) != nil {
		t.Error("nothing playing: no play")
	}

	tr.Begin(Play{Duration: 3 * time.Minute})
	tr.Position(2*time.Minute + 57*time.Second) // seeked near the end
	if p := tr.Finish(Skipped); p.EndReason != Finished {
		t.Errorf("within 5 s of the end counts as finished, got %s", p.EndReason)
	}
}

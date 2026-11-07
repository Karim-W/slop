package playlist

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreLifecycle(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Create("Road Trip"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("Road Trip"); err != ErrExists {
		t.Fatalf("duplicate name: %v", err)
	}
	n, err := s.Add("Road Trip",
		Entry{Path: "/music/a.flac", Title: "A - One", Duration: 61 * time.Second},
		Entry{Path: "/music/b.flac", Title: "B - Two"},
		Entry{Path: "/music/a.flac"}) // duplicate
	if err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	s.Move("Road Trip", 1, 0)
	p, _ := s.Get("Road Trip")
	if p.Entries[0].Path != "/music/b.flac" || p.Entries[1].Duration != 61*time.Second {
		t.Fatalf("%+v", p.Entries)
	}
	raw, _ := os.ReadFile(p.File)
	want := "#EXTM3U\n#PLAYLIST:Road Trip\n#EXTINF:-1,B - Two\n/music/b.flac\n#EXTINF:61,A - One\n/music/a.flac\n"
	if string(raw) != want {
		t.Fatalf("file:\n%s", raw)
	}
	if err := s.Rename("Road Trip", "Night Drive"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "Road Trip.m3u8")); err == nil {
		t.Fatal("old file left behind")
	}
	out, err := s.Export("Night Drive", t.TempDir())
	if err != nil || filepath.Base(out) != "Night Drive.m3u8" {
		t.Fatalf("%s %v", out, err)
	}
	s.Remove("Night Drive", 0)
	s.Delete("Night Drive")
	if ps, _ := s.List(); len(ps) != 0 {
		t.Fatal("delete")
	}
}

func TestImportResolvesRelativeAndFileURLs(t *testing.T) {
	src := t.TempDir()
	m3u := filepath.Join(src, "mix.m3u")
	os.WriteFile(m3u, []byte("\ufeff#EXTM3U\r\nsongs/one.mp3\r\nfile:///music/My%20Song.flac\r\n/abs/three.ogg\r\n"), 0o644)
	s := Store{Dir: t.TempDir()}
	p, err := s.Import(m3u)
	if err != nil {
		t.Fatal(err)
	}
	got := p.Paths()
	want := []string{filepath.Join(src, "songs/one.mp3"), "/music/My Song.flac", "/abs/three.ogg"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%d: %q want %q", i, got[i], want[i])
		}
	}
	if _, err := s.Import(m3u); err != nil {
		t.Fatal(err)
	}
	if ps, _ := s.List(); len(ps) != 2 || ps[1].Name != "mix 2" {
		t.Fatalf("second import should not overwrite: %+v", ps)
	}
}

func TestFileNameIsSafe(t *testing.T) {
	for in, want := range map[string]string{"a/b:c": "a-b-c.m3u8", "..hidden": "hidden.m3u8", "  ": "playlist.m3u8"} {
		if got := FileName(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

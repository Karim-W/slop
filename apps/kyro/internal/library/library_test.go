package library

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// flacHeader is a FLAC file with only a STREAMINFO block: enough to probe.
func flacHeader(rate, channels, bits int, samples uint64) []byte {
	si := make([]byte, 34)
	si[10] = byte(rate >> 12)
	si[11] = byte(rate >> 4)
	si[12] = byte(rate<<4) | byte((channels-1)<<1) | byte((bits-1)>>4)
	si[13] = byte((bits-1)<<4) | byte(samples>>32)
	binary.BigEndian.PutUint32(si[14:], uint32(samples))
	return append([]byte{'f', 'L', 'a', 'C', 0x80, 0, 0, 34}, si...)
}

func wavHeader(rate, channels, bits int, frames int) []byte {
	data := frames * channels * bits / 8
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+data))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], uint16(channels))
	binary.LittleEndian.PutUint32(h[24:], uint32(rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(rate*channels*bits/8))
	binary.LittleEndian.PutUint16(h[32:], uint16(channels*bits/8))
	binary.LittleEndian.PutUint16(h[34:], uint16(bits))
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(data))
	return append(h, make([]byte, data)...)
}

func write(t *testing.T, path string, b []byte) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProbeHiResFLACAndWAV(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.flac")
	write(t, f, flacHeader(192000, 6, 24, 192000*90))
	tr, err := Read(f)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Quality() != "FLAC 24/192 5.1" || tr.Duration != 90*time.Second || !tr.HiRes() {
		t.Fatalf("%s %v", tr.Quality(), tr.Duration)
	}
	w := filepath.Join(dir, "b.wav")
	write(t, w, wavHeader(44100, 2, 16, 44100*2))
	tr, _ = Read(w)
	if tr.Quality() != "WAV 16/44.1" || tr.Duration != 2*time.Second || tr.HiRes() {
		t.Fatalf("%s %v", tr.Quality(), tr.Duration)
	}
}

func TestTagsFallBackToFileNames(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Night Bus - Lowlight.wav")
	write(t, p, wavHeader(48000, 2, 16, 10))
	tr, _ := Read(p)
	if tr.Artist != "Night Bus" || tr.Title != "Lowlight" {
		t.Fatalf("%+v", tr)
	}
	p = filepath.Join(dir, "07. Intro.flac")
	write(t, p, flacHeader(44100, 2, 16, 10))
	tr, _ = Read(p)
	if tr.Title != "Intro" || tr.TrackNo != 7 {
		t.Fatalf("%+v", tr)
	}
}

func TestScanReferencesFilesAndTracksChanges(t *testing.T) {
	music := t.TempDir()
	a := filepath.Join(music, "Artist", "Album", "01 One.flac")
	b := filepath.Join(music, "Artist", "Album", "02 Two.flac")
	write(t, a, flacHeader(44100, 2, 16, 44100))
	write(t, b, flacHeader(44100, 2, 16, 44100))
	write(t, filepath.Join(music, "notes.txt"), []byte("not music"))
	write(t, filepath.Join(music, ".hidden", "x.flac"), flacHeader(44100, 2, 16, 1))

	path := filepath.Join(t.TempDir(), "library.json")
	lib, _ := Open(path)
	if r := lib.Scan([]string{music}, nil); r.Added != 2 || r.Total != 2 {
		t.Fatalf("first scan %+v", r)
	}
	if tr, ok := lib.ByPath(a); !ok || tr.Path != a {
		t.Fatal("tracks must reference the original file")
	}
	if r := lib.Scan([]string{music}, nil); r.Added+r.Updated != 0 {
		t.Fatalf("unchanged rescan did work: %+v", r)
	}
	os.Remove(b)
	if r := lib.Scan([]string{music}, nil); r.Missing != 1 {
		t.Fatalf("missing not noticed: %+v", r)
	}
	if tr, _ := lib.ByPath(b); !tr.Missing {
		t.Fatal("deleted file should stay, marked missing")
	}
	// An offline source (unplugged drive) is not "missing music".
	if r := lib.Scan(nil, nil); r.Missing != 0 {
		t.Fatalf("%+v", r)
	}
	lib.Save()
	again, _ := Open(path)
	if len(again.All()) != 2 {
		t.Fatal("library.json round trip")
	}
	if n := again.RemoveSource(music); n != 2 {
		t.Fatalf("removed %d", n)
	}
	if _, err := os.Stat(a); err != nil {
		t.Fatal("removing a source must not touch files")
	}
}

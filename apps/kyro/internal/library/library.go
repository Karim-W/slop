// Package library indexes music by reference. Importing a folder records
// where each file lives and what is in it; the audio itself is never copied.
package library

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dhowden/tag"

	"github.com/karim-w/slop/pkg/infra/jsonfile"
)

// Track is one audio file and what kyro knows about it.
type Track struct {
	ID          string        `json:"id"`
	Path        string        `json:"path"`
	Title       string        `json:"title"`
	Artist      string        `json:"artist"`
	Album       string        `json:"album"`
	AlbumArtist string        `json:"albumArtist,omitempty"`
	Genre       string        `json:"genre,omitempty"`
	Year        int           `json:"year,omitempty"`
	TrackNo     int           `json:"track,omitempty"`
	DiscNo      int           `json:"disc,omitempty"`
	Duration    time.Duration `json:"duration"`
	Codec       string        `json:"codec"`
	SampleRate  int           `json:"sampleRate,omitempty"`
	BitDepth    int           `json:"bitDepth,omitempty"`
	Channels    int           `json:"channels,omitempty"`
	EmbeddedArt bool          `json:"embeddedArt,omitempty"`
	Size        int64         `json:"size"`
	ModTime     time.Time     `json:"modTime"`
	// Missing is set when the file is gone from disk; kyro keeps the entry so
	// playlists don't silently lose it.
	Missing bool `json:"missing,omitempty"`
}

// IDFor is the stable ID of the file at path.
func IDFor(path string) string {
	h := sha1.Sum([]byte(path))
	return hex.EncodeToString(h[:8])
}

// SortArtist is the artist used for grouping albums.
func (t Track) SortArtist() string {
	if t.AlbumArtist != "" {
		return t.AlbumArtist
	}
	return t.Artist
}

// Quality is a short label like "FLAC 24/96 5.1" or "MP3 44.1".
func (t Track) Quality() string {
	return Quality(t.Codec, t.SampleRate, t.BitDepth, t.Channels)
}

// HiRes reports whether the stream is beyond CD quality.
func (t Track) HiRes() bool { return t.BitDepth > 16 || t.SampleRate > 48000 }

// Quality formats stream details compactly.
func Quality(codec string, rate, bits, channels int) string {
	parts := []string{codec}
	if rate > 0 {
		khz := strconv.FormatFloat(float64(rate)/1000, 'f', -1, 64)
		if bits > 0 {
			parts = append(parts, fmt.Sprintf("%d/%s", bits, khz))
		} else {
			parts = append(parts, khz)
		}
	}
	switch {
	case channels == 1:
		parts = append(parts, "mono")
	case channels == 6:
		parts = append(parts, "5.1")
	case channels == 8:
		parts = append(parts, "7.1")
	case channels > 2:
		parts = append(parts, fmt.Sprintf("%dch", channels))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// Extensions kyro indexes. Playback goes through mpv, which decodes all of
// them, including ALAC, DSD, WavPack and Monkey's Audio.
var Extensions = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".alac": true, ".aac": true, ".mp4": true,
	".ogg": true, ".oga": true, ".opus": true, ".wav": true, ".aif": true, ".aiff": true,
	".aifc": true, ".wv": true, ".ape": true, ".dsf": true, ".dff": true, ".wma": true,
	".mka": true, ".caf": true, ".ac3": true, ".eac3": true, ".dts": true, ".mpc": true,
}

// IsAudio reports whether path has a supported audio extension.
func IsAudio(path string) bool { return Extensions[strings.ToLower(filepath.Ext(path))] }

// ─── Library ────────────────────────────────────────────────────────────────

// Library is the index, persisted as library.json.
type Library struct {
	mu     sync.RWMutex
	path   string
	Tracks map[string]*Track `json:"tracks"`
}

// Open loads the index at path; a missing file is an empty library.
func Open(path string) (*Library, error) {
	l := &Library{path: path, Tracks: map[string]*Track{}}
	if _, err := jsonfile.Load(path, l); err != nil {
		return l, err
	}
	if l.Tracks == nil {
		l.Tracks = map[string]*Track{}
	}
	return l, nil
}

// Save writes library.json.
func (l *Library) Save() error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return jsonfile.Save(l.path, l, 0o644)
}

// Get returns the track with id.
func (l *Library) Get(id string) (Track, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	t, ok := l.Tracks[id]
	if !ok {
		return Track{}, false
	}
	return *t, true
}

// ByPath returns the track for a file path.
func (l *Library) ByPath(path string) (Track, bool) { return l.Get(IDFor(path)) }

// Update replaces a track, for details learned while playing.
func (l *Library) Update(t Track) {
	l.mu.Lock()
	l.Tracks[t.ID] = &t
	l.mu.Unlock()
}

// All returns every track, sorted by artist, album, disc and track number.
func (l *Library) All() []Track {
	l.mu.RLock()
	out := make([]Track, 0, len(l.Tracks))
	for _, t := range l.Tracks {
		out = append(out, *t)
	}
	l.mu.RUnlock()
	SortTracks(out)
	return out
}

// SortTracks orders tracks as an album-oriented library does.
func SortTracks(ts []Track) {
	slices.SortStableFunc(ts, func(a, b Track) int {
		for _, c := range []int{
			strings.Compare(strings.ToLower(a.SortArtist()), strings.ToLower(b.SortArtist())),
			strings.Compare(strings.ToLower(a.Album), strings.ToLower(b.Album)),
			a.DiscNo - b.DiscNo,
			a.TrackNo - b.TrackNo,
			strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)),
		} {
			if c != 0 {
				return c
			}
		}
		return strings.Compare(a.Path, b.Path)
	})
}

// ScanResult summarises a scan.
type ScanResult struct {
	Added, Updated, Missing, Total int
	Errors                         []error
}

// Scan walks every source, indexing new and changed files and marking
// vanished ones missing. Unchanged files (same size and modification time)
// are not reopened, so rescans are fast. progress, if not nil, is called
// with the number of files seen so far.
func (l *Library) Scan(sources []string, progress func(seen int)) ScanResult {
	var res ScanResult
	seen := map[string]bool{}
	count := 0
	visit := func(path string, info fs.FileInfo) {
		if !IsAudio(path) {
			return
		}
		count++
		if progress != nil && count%50 == 0 {
			progress(count)
		}
		id := IDFor(path)
		seen[id] = true
		l.mu.RLock()
		old, ok := l.Tracks[id]
		l.mu.RUnlock()
		if ok && !old.Missing && old.Size == info.Size() && old.ModTime.Equal(info.ModTime()) {
			return
		}
		t, err := Read(path)
		if err != nil {
			res.Errors = append(res.Errors, err)
			return
		}
		if ok && old.Duration > 0 && t.Duration == 0 {
			t.Duration = old.Duration // learned during playback earlier
		}
		l.Update(t)
		if ok {
			res.Updated++
		} else {
			res.Added++
		}
	}
	scanned := map[string]bool{}
	for _, src := range sources {
		src = filepath.Clean(src)
		info, err := os.Stat(src)
		if err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		scanned[src] = true
		if !info.IsDir() {
			visit(src, info)
			continue
		}
		filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable folder: skip it, keep scanning
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && p != src {
					return filepath.SkipDir
				}
				return nil
			}
			if fi, err := d.Info(); err == nil && fi.Mode().IsRegular() {
				visit(p, fi)
			}
			return nil
		})
	}
	l.mu.Lock()
	for id, t := range l.Tracks {
		if seen[id] {
			continue
		}
		if !underAny(t.Path, scanned) {
			// The source was removed, or is offline (an unplugged drive):
			// leave its tracks alone.
			continue
		}
		if !t.Missing {
			t.Missing = true
			res.Missing++
		}
	}
	res.Total = len(l.Tracks)
	l.mu.Unlock()
	return res
}

func underAny(path string, roots map[string]bool) bool {
	for r := range roots {
		if path == r || strings.HasPrefix(path, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// RemoveSource drops every track under src from the index.
func (l *Library) RemoveSource(src string) int {
	src = filepath.Clean(src)
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for id, t := range l.Tracks {
		if underAny(t.Path, map[string]bool{src: true}) {
			delete(l.Tracks, id)
			n++
		}
	}
	return n
}

// Read builds a Track from a file's tags and stream headers.
func Read(path string) (Track, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Track{}, err
	}
	t := Track{ID: IDFor(path), Path: path, Size: info.Size(), ModTime: info.ModTime()}
	if s, err := probe(path); err == nil {
		t.Codec, t.SampleRate, t.BitDepth, t.Channels, t.Duration = s.Codec, s.SampleRate, s.BitDepth, s.Channels, s.Duration
	}
	if t.Codec == "" {
		t.Codec = strings.ToUpper(strings.TrimPrefix(filepath.Ext(path), "."))
	}
	if f, err := os.Open(path); err == nil {
		if m, err := tag.ReadFrom(f); err == nil {
			t.Title, t.Artist, t.Album = strings.TrimSpace(m.Title()), strings.TrimSpace(m.Artist()), strings.TrimSpace(m.Album())
			t.AlbumArtist, t.Genre, t.Year = strings.TrimSpace(m.AlbumArtist()), strings.TrimSpace(m.Genre()), m.Year()
			t.TrackNo, _ = m.Track()
			t.DiscNo, _ = m.Disc()
			t.EmbeddedArt = m.Picture() != nil
		}
		f.Close()
	}
	fillFromPath(&t)
	return t, nil
}

// fillFromPath guesses missing tags from "Artist/Album/01 Title.ext" or
// "Artist - Title.ext".
func fillFromPath(t *Track) {
	base := strings.TrimSuffix(filepath.Base(t.Path), filepath.Ext(t.Path))
	if t.Title == "" {
		title := base
		if a, b, ok := strings.Cut(base, " - "); ok {
			title = b
			if t.Artist == "" {
				t.Artist = strings.TrimSpace(a)
			}
		}
		// strip a leading track number: "01 Title", "01. Title", "1-03 Title"
		if i := strings.IndexFunc(title, func(r rune) bool { return !(r >= '0' && r <= '9' || r == '-' || r == '.') }); i > 0 && i < len(title) && title[i] == ' ' {
			if t.TrackNo == 0 {
				n, _ := strconv.Atoi(strings.TrimRight(title[:i], "."))
				t.TrackNo = n
			}
			title = title[i+1:]
		}
		t.Title = strings.TrimSpace(title)
	}
}

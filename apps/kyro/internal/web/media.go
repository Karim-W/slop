package web

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"

	"github.com/karim-w/slop/apps/kyro/internal/art"
	"github.com/karim-w/slop/apps/kyro/internal/library"
)

// media serves songs and covers. A browser only ever names a track by
// ID; media maps IDs to files kyro knows (the library, and songs the
// player handed to a browser), so no request can reach any other file.
type media struct {
	lib      *library.Library
	mpv      string
	cacheDir string

	mu      sync.Mutex
	allowed map[string]string // track ID → path, for songs outside the library
	known   map[string]library.Track
	busy    map[string]*job
	covers  map[string][]byte // resized covers by key+size
	coverQ  []string
}

type job struct {
	done chan struct{}
	err  error
}

func newMedia(lib *library.Library, mpv, cacheDir string) *media {
	return &media{lib: lib, mpv: mpv, cacheDir: cacheDir, allowed: map[string]string{}, known: map[string]library.Track{},
		busy: map[string]*job{}, covers: map[string][]byte{}}
}

// allow lets browsers fetch path and returns its ID.
func (m *media) allow(path string) string {
	id := library.IDFor(path)
	m.mu.Lock()
	m.allowed[id] = path
	m.mu.Unlock()
	return id
}

// remember records tracks shown to browsers (the queue, playlists) so
// their covers can be served.
func (m *media) remember(ts []library.Track) {
	m.mu.Lock()
	for _, t := range ts {
		m.known[t.ID] = t
	}
	if len(m.known) > 20000 {
		clear(m.known)
	}
	m.mu.Unlock()
}

// path is the file behind a track ID, "" if kyro doesn't know it.
func (m *media) path(id string) string {
	t, ok := m.track(id)
	if !ok {
		return ""
	}
	return t.Path
}

func (m *media) track(id string) (library.Track, bool) {
	if t, ok := m.lib.Get(id); ok {
		return t, true
	}
	m.mu.Lock()
	t, ok := m.known[id]
	p, allowed := m.allowed[id]
	m.mu.Unlock()
	if ok {
		return t, true
	}
	if allowed {
		t, err := library.Read(p)
		if err != nil {
			return library.Track{}, false
		}
		m.remember([]library.Track{t})
		return t, true
	}
	return library.Track{}, false
}

// kinds a browser may play natively, by what canPlayType reports.
var mimes = map[string]string{
	"mp3": "audio/mpeg", "aac": "audio/mp4", "adts": "audio/aac", "alac": "audio/mp4", "flac": "audio/flac",
	"opus": "audio/ogg", "vorbis": "audio/ogg", "wav": "audio/wav", "aiff": "audio/aiff",
}

// Kind names a track's format the way browsers are asked about it, ""
// for formats no browser plays (DSD, WavPack, APE, AC-3 …).
func Kind(t library.Track) string {
	switch strings.ToLower(filepath.Ext(t.Path)) {
	case ".mp3":
		return "mp3"
	case ".flac":
		return "flac"
	case ".m4a", ".mp4", ".alac":
		switch t.Codec {
		case "ALAC":
			return "alac"
		case "AAC":
			return "aac"
		}
	case ".aac":
		return "adts"
	case ".ogg", ".oga":
		if t.Codec == "Opus" {
			return "opus"
		}
		return "vorbis"
	case ".opus":
		return "opus"
	case ".wav":
		return "wav"
	case ".aif", ".aiff", ".aifc":
		return "aiff"
	}
	return ""
}

// audio sends a song in a format the browser plays: the file itself when
// it can, otherwise a lossless copy kyro makes with mpv (FLAC, or WAV for
// browsers without FLAC), cached for next time.
func (m *media) audio(w http.ResponseWriter, r *http.Request, id string, formats map[string]bool, af string) {
	t, ok := m.track(id)
	if !ok || t.Missing {
		http.NotFound(w, r)
		return
	}
	file, mime := t.Path, ""
	if k := Kind(t); af == "" && k != "" && (formats[k] || formats["adts"] && k == "aac") {
		mime = mimes[k]
	} else {
		target := "wav"
		if formats["flac"] {
			target = "flac"
		}
		out, err := m.transcode(r.Context(), t, target, af)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
			return
		}
		file, mime = out, mimes[target]
	}
	f, err := os.Open(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// transcode converts t into the cache once, however many ask at once.
func (m *media) transcode(ctx context.Context, t library.Track, format, af string) (string, error) {
	if m.mpv == "" {
		return "", errors.New("this browser can't play " + t.Codec + ", and mpv isn't there to convert it")
	}
	h := sha1.Sum([]byte(fmt.Sprintf("%s\x00%d\x00%d\x00%s", t.Path, t.Size, t.ModTime.UnixNano(), af)))
	out := filepath.Join(m.cacheDir, hex.EncodeToString(h[:10])+"."+format)
	if _, err := os.Stat(out); err == nil {
		now := time.Now()
		os.Chtimes(out, now, now) // recently used: pruned last
		return out, nil
	}
	m.mu.Lock()
	j, running := m.busy[out]
	if !running {
		j = &job{done: make(chan struct{})}
		m.busy[out] = j
		go func() {
			j.err = m.convert(t.Path, out, format, t.SampleRate, af)
			m.mu.Lock()
			delete(m.busy, out)
			m.mu.Unlock()
			close(j.done)
		}()
	}
	m.mu.Unlock()
	select {
	case <-j.done:
		return out, j.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (m *media) convert(src, out, format string, rate int, af string) error {
	if err := os.MkdirAll(m.cacheDir, 0o700); err != nil {
		return err
	}
	tmp := out + ".part"
	defer os.Remove(tmp)
	codec := "flac"
	if format == "wav" {
		codec = "pcm_s16le"
	}
	args := []string{"--no-config", "--load-scripts=no", "--no-video", "--audio-display=no", "--no-terminal",
		"--o=" + tmp, "--of=" + format, "--oac=" + codec}
	if af != "" {
		args = append(args, "--af="+af) // the sound mode; it ends in its own limiter
	}
	if ext := strings.ToLower(filepath.Ext(src)); rate > 192000 || ext == ".dsf" || ext == ".dff" {
		args = append(args, "--audio-samplerate=88200") // DSD: browsers can't take 2.8 MHz
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.mpv, append(args, "--", src)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("converting for the browser: %v %s", err, strings.TrimSpace(string(b)))
	}
	if info, err := os.Stat(tmp); err != nil || info.Size() == 0 {
		return errors.New("converting for the browser produced nothing")
	}
	return os.Rename(tmp, out)
}

// prune keeps the conversion cache under limit bytes, dropping the least
// recently used, and anything unused for a month.
func (m *media) prune(limit int64) {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		return
	}
	type f struct {
		path string
		size int64
		at   time.Time
	}
	var fs []f
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		p := filepath.Join(m.cacheDir, e.Name())
		if strings.HasSuffix(e.Name(), ".part") || time.Since(info.ModTime()) > 30*24*time.Hour {
			os.Remove(p)
			continue
		}
		fs = append(fs, f{p, info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].at.Before(fs[j].at) })
	for _, x := range fs {
		if total <= limit {
			break
		}
		os.Remove(x.path)
		total -= x.size
	}
}

// cover sends a track's art, resized to size×size when size > 0.
func (m *media) cover(w http.ResponseWriter, r *http.Request, id string, size int) {
	t, ok := m.track(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	size = min(max(size, 0), 1600)
	etag := `"` + art.Key(t) + "-" + strconv.Itoa(size) + `"`
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	key := etag
	m.mu.Lock()
	b, cached := m.covers[key]
	m.mu.Unlock()
	if !cached {
		data, _, ok := art.Source(t)
		if !ok {
			http.NotFound(w, r)
			return
		}
		b = data
		if size > 0 {
			if small, err := shrink(data, size); err == nil {
				b = small
			}
		}
		m.mu.Lock()
		m.covers[key] = b
		m.coverQ = append(m.coverQ, key)
		for len(m.coverQ) > 400 {
			delete(m.covers, m.coverQ[0])
			m.coverQ = slices.Delete(m.coverQ, 0, 1)
		}
		m.mu.Unlock()
	}
	w.Header().Set("Content-Type", http.DetectContentType(b))
	w.Write(b)
}

// shrink scales a cover to fill size×size, cropping the longer side.
func shrink(data []byte, size int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	if side <= size {
		size = side
	}
	crop := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, 0, 0)
	crop.Max = crop.Min.Add(image.Pt(side, side))
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, crop, draw.Src, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 86}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Package playlist keeps playlists as extended M3U files (.m3u8, UTF-8) in
// ~/.kyro/playlists. Entries are absolute paths to the original files, so a
// playlist can be imported as-is into iTunes / Apple Music, and from there
// synced to an iPod.
package playlist

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Entry is one line of a playlist.
type Entry struct {
	Path     string
	Title    string // "Artist - Title" from #EXTINF, informational
	Duration time.Duration
}

// Playlist is a named, ordered list of files.
type Playlist struct {
	Name    string
	File    string
	Entries []Entry
}

// Paths returns the entries' file paths.
func (p Playlist) Paths() []string {
	out := make([]string, len(p.Entries))
	for i, e := range p.Entries {
		out[i] = e.Path
	}
	return out
}

// Store manages the playlists in one folder.
type Store struct{ Dir string }

// ErrExists means a playlist with that name already exists.
var ErrExists = errors.New("a playlist with that name already exists")

// List loads every playlist, sorted by name.
func (s Store) List() ([]Playlist, error) {
	files, err := filepath.Glob(filepath.Join(s.Dir, "*.m3u8"))
	if err != nil {
		return nil, err
	}
	m3u, _ := filepath.Glob(filepath.Join(s.Dir, "*.m3u"))
	files = append(files, m3u...)
	var out []Playlist
	for _, f := range files {
		p, err := ReadFile(f)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Playlist) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out, nil
}

// Get loads the playlist called name.
func (s Store) Get(name string) (Playlist, error) { return ReadFile(s.file(name)) }

func (s Store) file(name string) string { return filepath.Join(s.Dir, FileName(name)) }

// Create makes an empty playlist.
func (s Store) Create(name string) (Playlist, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Playlist{}, errors.New("a playlist needs a name")
	}
	if _, err := os.Stat(s.file(name)); err == nil {
		return Playlist{}, ErrExists
	}
	p := Playlist{Name: name, File: s.file(name)}
	return p, s.Save(p)
}

// Save writes p to its file.
func (s Store) Save(p Playlist) error {
	if p.File == "" {
		p.File = s.file(p.Name)
	}
	if err := os.MkdirAll(filepath.Dir(p.File), 0o755); err != nil {
		return err
	}
	tmp := p.File + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := Write(f, p); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p.File)
}

// Rename gives a playlist a new name (and file).
func (s Store) Rename(old, name string) error {
	p, err := s.Get(old)
	if err != nil {
		return err
	}
	if _, err := os.Stat(s.file(name)); err == nil && !strings.EqualFold(FileName(old), FileName(name)) {
		return ErrExists
	}
	oldFile := p.File
	p.Name, p.File = name, s.file(name)
	if err := s.Save(p); err != nil {
		return err
	}
	if oldFile != p.File {
		return os.Remove(oldFile)
	}
	return nil
}

// Delete removes a playlist. The music files are untouched.
func (s Store) Delete(name string) error { return os.Remove(s.file(name)) }

// Add appends entries to a playlist, skipping ones already in it.
func (s Store) Add(name string, entries ...Entry) (added int, err error) {
	p, err := s.Get(name)
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, e := range p.Entries {
		have[e.Path] = true
	}
	for _, e := range entries {
		if have[e.Path] {
			continue
		}
		have[e.Path] = true
		p.Entries = append(p.Entries, e)
		added++
	}
	return added, s.Save(p)
}

// Remove drops the entry at index i.
func (s Store) Remove(name string, i int) error {
	p, err := s.Get(name)
	if err != nil {
		return err
	}
	if i < 0 || i >= len(p.Entries) {
		return fmt.Errorf("no entry %d", i)
	}
	p.Entries = slices.Delete(p.Entries, i, i+1)
	return s.Save(p)
}

// Move moves the entry at from to index to.
func (s Store) Move(name string, from, to int) error {
	p, err := s.Get(name)
	if err != nil {
		return err
	}
	if from < 0 || from >= len(p.Entries) || to < 0 || to >= len(p.Entries) {
		return nil
	}
	e := p.Entries[from]
	p.Entries = slices.Delete(p.Entries, from, from+1)
	p.Entries = slices.Insert(p.Entries, to, e)
	return s.Save(p)
}

// Import copies an existing .m3u/.m3u8 into the store, resolving relative
// entries against the source file's folder so they keep working.
func (s Store) Import(path string) (Playlist, error) {
	p, err := ReadFile(path)
	if err != nil {
		return Playlist{}, err
	}
	base := p.Name
	for i := 2; ; i++ {
		if _, err := os.Stat(s.file(p.Name)); errors.Is(err, os.ErrNotExist) {
			break
		}
		p.Name = fmt.Sprintf("%s %d", base, i)
	}
	p.File = s.file(p.Name)
	return p, s.Save(p)
}

// Export copies a playlist to dest (a file, or a folder to put it in).
func (s Store) Export(name, dest string) (string, error) {
	p, err := s.Get(name)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(dest); err == nil && fi.IsDir() {
		dest = filepath.Join(dest, FileName(p.Name))
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return dest, Write(f, p)
}

// FileName turns a playlist name into a safe file name.
func FileName(name string) string {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
	clean = strings.TrimLeft(clean, ".")
	if clean == "" {
		clean = "playlist"
	}
	return clean + ".m3u8"
}

// ─── Format ─────────────────────────────────────────────────────────────────

// Write encodes p as extended M3U.
func Write(w io.Writer, p Playlist) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw, "#EXTM3U")
	fmt.Fprintf(bw, "#PLAYLIST:%s\n", p.Name)
	for _, e := range p.Entries {
		secs := -1
		if e.Duration > 0 {
			secs = int(e.Duration.Round(time.Second) / time.Second)
		}
		fmt.Fprintf(bw, "#EXTINF:%d,%s\n", secs, strings.ReplaceAll(e.Title, "\n", " "))
		fmt.Fprintln(bw, e.Path)
	}
	return bw.Flush()
}

// ReadFile decodes an .m3u/.m3u8 file. Relative entries resolve against
// the file's folder; file:// URLs become paths.
func ReadFile(path string) (Playlist, error) {
	f, err := os.Open(path)
	if err != nil {
		return Playlist{}, err
	}
	defer f.Close()
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	p := Playlist{Name: name, File: path}
	dir := filepath.Dir(path)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var pending Entry
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		switch {
		case line == "":
		case strings.HasPrefix(line, "#PLAYLIST:"):
			if n := strings.TrimSpace(strings.TrimPrefix(line, "#PLAYLIST:")); n != "" {
				p.Name = n
			}
		case strings.HasPrefix(line, "#EXTINF:"):
			info := strings.TrimPrefix(line, "#EXTINF:")
			secs, title, _ := strings.Cut(info, ",")
			var n int
			fmt.Sscanf(secs, "%d", &n)
			pending = Entry{Title: title}
			if n > 0 {
				pending.Duration = time.Duration(n) * time.Second
			}
		case strings.HasPrefix(line, "#"):
		default:
			e := pending
			pending = Entry{}
			e.Path = resolve(dir, line)
			p.Entries = append(p.Entries, e)
		}
	}
	return p, sc.Err()
}

func resolve(dir, entry string) string {
	if strings.HasPrefix(entry, "file://") {
		if u, err := url.Parse(entry); err == nil {
			entry = u.Path
		}
	}
	if filepath.IsAbs(entry) {
		return filepath.Clean(entry)
	}
	return filepath.Join(dir, filepath.FromSlash(strings.ReplaceAll(entry, "\\", "/")))
}

// Package stats records what you listen to, for kyro's year in review.
//
// Every play becomes one row: the song, when and how long you listened,
// how it ended (finished, skipped, stopped), and the setting it was played
// in. Loves, ratings and playlist additions are logged as events. Nothing
// is summarised away, so a later year in review can compute anything from
// the raw traces.
//
// The data stays on this machine and only kyro can read it:
//
//   - The database is SQLite, encrypted page by page with Adiantum (the
//     "adiantum" VFS of github.com/ncruces/go-sqlite3). The schema, the
//     rows and the journal are all ciphertext on disk.
//   - The 256-bit key is random and lives in the system keychain (see
//     LoadKey), never in a file next to the database.
//   - The folder is private to your user (0700) and the file 0600.
//   - Nothing is ever sent anywhere: this package has no network code.
package stats

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"       // database/sql driver "sqlite3"
	_ "github.com/ncruces/go-sqlite3/vfs/adiantum" // the encrypting VFS
)

// ErrWrongKey means the database can't be decrypted with the key given.
var ErrWrongKey = errors.New("the listening stats can't be decrypted with this key")

// End reasons of a play.
const (
	Finished = "finished" // played to the end
	Skipped  = "skipped"  // another song started before the end
	Stopped  = "stopped"  // playback was stopped
	Quit     = "quit"     // kyro closed while it played
	Failed   = "error"    // the file couldn't be played
)

// Play is one listen of one song.
type Play struct {
	Start, End time.Time
	TrackID    string
	Path       string
	Title      string
	Artist     string
	Album      string
	// AlbumArtist and Genre come from the tags, when present.
	AlbumArtist, Genre string
	Year               int
	Duration           time.Duration // the song's length
	Listened           time.Duration // time actually heard: no pauses, no seeked-over parts
	EndPos             time.Duration // where it stopped
	EndReason          string        // Finished, Skipped, Stopped, Quit or Failed
	// Context is where it was started from: songs, albums, artists, queue,
	// loved or playlist; ContextName names the album, artist or playlist.
	Context, ContextName string
	Shuffle              bool
	Repeat               string
	Sound                string // the sound mode
	Codec                string
	SampleRate           int
	BitDepth             int
	Channels             int
	Pauses, Seeks        int
	Loved                bool // at the time of the play
	Stars                int
}

// Event is something done to a song outside playback.
type Event struct {
	At                                  time.Time
	Kind                                string // "love", "unlove", "rate", "playlist-add"
	Value                               string // the stars for rate, the playlist for playlist-add
	TrackID, Path, Title, Artist, Album string
}

// Store is the encrypted database. Writes are queued and applied in order
// on a background goroutine, so recording never holds up the player.
type Store struct {
	db     *sql.DB
	path   string
	queue  chan func(*sql.DB) error
	done   chan struct{}
	mu     sync.Mutex
	closed bool
	errs   []error
}

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS plays (
	id           INTEGER PRIMARY KEY,
	started_at   INTEGER NOT NULL, -- unix milliseconds
	ended_at     INTEGER NOT NULL,
	tz_offset    INTEGER NOT NULL, -- minutes east of UTC when it started
	track_id     TEXT NOT NULL,
	path         TEXT NOT NULL,
	title        TEXT NOT NULL,
	artist       TEXT NOT NULL,
	album        TEXT NOT NULL,
	album_artist TEXT NOT NULL,
	genre        TEXT NOT NULL,
	year         INTEGER NOT NULL,
	duration_ms  INTEGER NOT NULL,
	listened_ms  INTEGER NOT NULL,
	end_pos_ms   INTEGER NOT NULL,
	end_reason   TEXT NOT NULL,
	context      TEXT NOT NULL,
	context_name TEXT NOT NULL,
	shuffle      INTEGER NOT NULL,
	repeat       TEXT NOT NULL,
	sound        TEXT NOT NULL,
	codec        TEXT NOT NULL,
	sample_rate  INTEGER NOT NULL,
	bit_depth    INTEGER NOT NULL,
	channels     INTEGER NOT NULL,
	pauses       INTEGER NOT NULL,
	seeks        INTEGER NOT NULL,
	loved        INTEGER NOT NULL,
	stars        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS plays_started ON plays (started_at);
CREATE INDEX IF NOT EXISTS plays_track ON plays (track_id);
CREATE TABLE IF NOT EXISTS events (
	id        INTEGER PRIMARY KEY,
	at        INTEGER NOT NULL,
	tz_offset INTEGER NOT NULL,
	kind      TEXT NOT NULL,
	value     TEXT NOT NULL,
	track_id  TEXT NOT NULL,
	path      TEXT NOT NULL,
	title     TEXT NOT NULL,
	artist    TEXT NOT NULL,
	album     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_at ON events (at);
`

// SchemaVersion is stored in meta; bump it with a migration when the
// tables change.
const SchemaVersion = "1"

// Open opens (or creates) the database at path with a 32-byte key.
func Open(path string, key []byte) (*Store, error) {
	if len(key) != 32 {
		return nil, errors.New("stats: the key must be 32 bytes")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	os.Chmod(filepath.Dir(path), 0o700)
	if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600); err == nil {
		f.Close() // an empty file is a new database; this sets its mode
	}
	q := url.Values{}
	q.Set("vfs", "adiantum")
	q.Set("hexkey", hex.EncodeToString(key))
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "temp_store(memory)") // temporary data never touches disk
	q.Add("_pragma", "secure_delete(on)")  // deleted rows are overwritten
	dsn := (&url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: q.Encode()}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		if strings.Contains(err.Error(), "not a database") {
			return nil, ErrWrongKey
		}
		return nil, fmt.Errorf("stats: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO meta VALUES ('schema', ?), ('created', ?)`,
		SchemaVersion, time.Now().UTC().Format(time.RFC3339)); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, path: path, queue: make(chan func(*sql.DB) error, 256), done: make(chan struct{})}
	go s.write()
	return s, nil
}

// Path is the database file.
func (s *Store) Path() string { return s.path }

func (s *Store) write() {
	defer close(s.done)
	for f := range s.queue {
		if err := f(s.db); err != nil {
			s.mu.Lock()
			s.errs = append(s.errs, err)
			s.mu.Unlock()
		}
	}
}

func (s *Store) enqueue(f func(*sql.DB) error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.queue <- f:
	default: // far behind: drop rather than block playback
		s.errs = append(s.errs, errors.New("stats: write queue full"))
	}
}

func ms(d time.Duration) int64 { return d.Milliseconds() }

func tz(t time.Time) int {
	_, off := t.Zone()
	return off / 60
}

func b(v bool) int {
	if v {
		return 1
	}
	return 0
}

// AddPlay records a play. A nil Store records nothing.
func (s *Store) AddPlay(p Play) {
	s.enqueue(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO plays (started_at, ended_at, tz_offset, track_id, path, title, artist,
			album, album_artist, genre, year, duration_ms, listened_ms, end_pos_ms, end_reason, context,
			context_name, shuffle, repeat, sound, codec, sample_rate, bit_depth, channels, pauses, seeks,
			loved, stars) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.Start.UnixMilli(), p.End.UnixMilli(), tz(p.Start), p.TrackID, p.Path, p.Title, p.Artist,
			p.Album, p.AlbumArtist, p.Genre, p.Year, ms(p.Duration), ms(p.Listened), ms(p.EndPos), p.EndReason,
			p.Context, p.ContextName, b(p.Shuffle), p.Repeat, p.Sound, p.Codec, p.SampleRate, p.BitDepth,
			p.Channels, p.Pauses, p.Seeks, b(p.Loved), p.Stars)
		return err
	})
}

// AddEvent records an event. A nil Store records nothing.
func (s *Store) AddEvent(e Event) {
	s.enqueue(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT INTO events (at, tz_offset, kind, value, track_id, path, title, artist, album)
			VALUES (?,?,?,?,?,?,?,?,?)`, e.At.UnixMilli(), tz(e.At), e.Kind, e.Value, e.TrackID, e.Path,
			e.Title, e.Artist, e.Album)
		return err
	})
}

// Close writes everything queued and closes the database, returning the
// first write error, if any.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.queue)
	}
	s.mu.Unlock()
	<-s.done
	err := s.db.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errs) > 0 {
		return s.errs[0]
	}
	return err
}

// Summary is a short account of what's recorded, for `kyro stats`. The
// year in review will read the raw tables instead.
type Summary struct {
	Since      time.Time // first play recorded
	Plays      int       // plays heard for at least 30 s (or to the end)
	Listened   time.Duration
	Songs      int // different songs among those plays
	TopArtist  string
	TopSong    string // "Title — Artist"
	TopSongN   int
	AllPlays   int // every row, skips included
	EventCount int
}

// Summarize reads the plays that started in [from, to).
func (s *Store) Summarize(from, to time.Time) (Summary, error) {
	var sum Summary
	// A play counts when you heard 30 s of it, or all of a shorter song.
	const counted = `(listened_ms >= 30000 OR end_reason = 'finished')`
	a, z := from.UnixMilli(), to.UnixMilli()
	var since sql.NullInt64
	var listened sql.NullInt64
	err := s.db.QueryRow(`SELECT MIN(started_at), COUNT(*) FROM plays`).Scan(&since, &sum.AllPlays)
	if err != nil {
		return sum, err
	}
	if since.Valid {
		sum.Since = time.UnixMilli(since.Int64)
	}
	err = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(listened_ms),0), COUNT(DISTINCT track_id) FROM plays
		WHERE started_at >= ? AND started_at < ? AND `+counted, a, z).Scan(&sum.Plays, &listened, &sum.Songs)
	if err != nil {
		return sum, err
	}
	sum.Listened = time.Duration(listened.Int64) * time.Millisecond
	s.db.QueryRow(`SELECT artist FROM plays WHERE started_at >= ? AND started_at < ? AND `+counted+`
		GROUP BY artist ORDER BY SUM(listened_ms) DESC LIMIT 1`, a, z).Scan(&sum.TopArtist)
	var title, artist string
	if s.db.QueryRow(`SELECT title, artist, COUNT(*) n FROM plays WHERE started_at >= ? AND started_at < ? AND `+counted+`
		GROUP BY track_id ORDER BY n DESC, SUM(listened_ms) DESC LIMIT 1`, a, z).Scan(&title, &artist, &sum.TopSongN) == nil {
		sum.TopSong = title + " — " + artist
	}
	err = s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE at >= ? AND at < ?`, a, z).Scan(&sum.EventCount)
	return sum, err
}

// Plays returns every play that started in [from, to), oldest first: the
// raw traces the year in review is built from.
func (s *Store) Plays(from, to time.Time) ([]Play, error) {
	rows, err := s.db.Query(`SELECT started_at, ended_at, track_id, path, title, artist, album, album_artist,
		genre, year, duration_ms, listened_ms, end_pos_ms, end_reason, context, context_name, shuffle, repeat,
		sound, codec, sample_rate, bit_depth, channels, pauses, seeks, loved, stars
		FROM plays WHERE started_at >= ? AND started_at < ? ORDER BY started_at, id`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Play
	for rows.Next() {
		var p Play
		var start, end, dur, heard, pos int64
		var shuffle, loved int
		if err := rows.Scan(&start, &end, &p.TrackID, &p.Path, &p.Title, &p.Artist, &p.Album, &p.AlbumArtist,
			&p.Genre, &p.Year, &dur, &heard, &pos, &p.EndReason, &p.Context, &p.ContextName, &shuffle, &p.Repeat,
			&p.Sound, &p.Codec, &p.SampleRate, &p.BitDepth, &p.Channels, &p.Pauses, &p.Seeks, &loved, &p.Stars); err != nil {
			return nil, err
		}
		p.Start, p.End = time.UnixMilli(start), time.UnixMilli(end)
		p.Duration, p.Listened, p.EndPos = time.Duration(dur)*time.Millisecond, time.Duration(heard)*time.Millisecond, time.Duration(pos)*time.Millisecond
		p.Shuffle, p.Loved = shuffle == 1, loved == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

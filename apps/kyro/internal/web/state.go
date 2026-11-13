package web

import (
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/art"
	"github.com/karim-w/slop/apps/kyro/internal/engine"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/apps/kyro/internal/ratings"
	"github.com/karim-w/slop/pkg/domain/theme"
)

// State is what every page shows: the player, the outputs and the
// colours kyro draws with right now (the cover's, with the dynamic theme).
type State struct {
	Server   string          `json:"server"` // this machine's name
	Active   bool            `json:"active"`
	Paused   bool            `json:"paused"`
	Track    *Track          `json:"track,omitempty"`
	Pos      float64         `json:"pos"` // seconds, at At
	At       int64           `json:"at"`  // Unix milliseconds
	Duration float64         `json:"duration"`
	Volume   int             `json:"volume"`
	Shuffle  bool            `json:"shuffle"`
	Repeat   string          `json:"repeat"`
	Sound    string          `json:"sound"`
	Quality  string          `json:"quality,omitempty"`
	Index    int             `json:"index"`
	QueueLen int             `json:"queueLen"`
	QueueRev string          `json:"queueRev"`
	Output   string          `json:"output"`
	Outputs  []engine.Output `json:"outputs"`
	Theme    theme.Theme     `json:"theme"`
	// Cover is a theme from the playing song's cover, for pages that take
	// their colours from it whatever the terminal does.
	Cover *theme.Theme `json:"cover,omitempty"`
	// Field is the cover's colours as a 3×3 grid, row by row, toned for
	// Cover's text: zen spreads them across the screen around the art.
	Field   []string `json:"field,omitempty"`
	Dynamic bool     `json:"dynamic"`
	// Revisions tell pages when to fetch the library or playlists again.
	LibraryRev   int `json:"libraryRev"`
	PlaylistsRev int `json:"playlistsRev"`
}

// Track is a song as pages see it: no file paths.
type Track struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist,omitempty"`
	Album       string  `json:"album,omitempty"`
	AlbumArtist string  `json:"albumArtist,omitempty"`
	Genre       string  `json:"genre,omitempty"`
	Year        int     `json:"year,omitempty"`
	No          int     `json:"no,omitempty"`
	Disc        int     `json:"disc,omitempty"`
	Duration    float64 `json:"duration"`
	Quality     string  `json:"quality,omitempty"`
	HiRes       bool    `json:"hires,omitempty"`
	Missing     bool    `json:"missing,omitempty"`
	Art         string  `json:"art"` // cover key: tracks of one album share it
	Loved       bool    `json:"loved,omitempty"`
	Stars       int     `json:"stars,omitempty"`
}

// TrackOf converts a library track.
func TrackOf(t library.Track, r ratings.Rating) Track {
	return Track{ID: t.ID, Title: t.Title, Artist: t.Artist, Album: t.Album, AlbumArtist: t.AlbumArtist,
		Genre: t.Genre, Year: t.Year, No: t.TrackNo, Disc: t.DiscNo, Duration: t.Duration.Seconds(),
		Quality: t.Quality(), HiRes: t.HiRes(), Missing: t.Missing, Art: art.Key(t), Loved: r.Loved, Stars: r.Stars}
}

// hub fans state and speaker commands out to open event streams.
type hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
	last    []byte // the newest state message
	key     string // the state without its position, to spot real changes
	pos     float64
	playing bool
	sent    time.Time
}

type client struct {
	device, pair string
	ch           chan []byte
}

func newHub() *hub { return &hub{clients: map[*client]struct{}{}} }

func (h *hub) join(device, pair string) *client {
	c := &client{device: device, pair: pair, ch: make(chan []byte, 64)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	if h.last != nil {
		c.ch <- h.last
	}
	h.mu.Unlock()
	return c
}

func (h *hub) leave(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// publish sends st when something changed. Position alone goes out when
// it jumps (a seek) or every twenty seconds; pages move the clock between,
// so an idle phone isn't woken several times a minute.
func (h *hub) publish(st State) {
	now := time.Now()
	st.At = now.UnixMilli()
	pos := st.Pos
	st.Pos, st.At = 0, 0
	k, _ := json.Marshal(st)
	st.Pos, st.At = pos, now.UnixMilli()
	h.mu.Lock()
	defer h.mu.Unlock()
	expect := h.pos
	if h.playing {
		expect += now.Sub(h.sent).Seconds()
	}
	if string(k) == h.key && math.Abs(pos-expect) < 1.5 && now.Sub(h.sent) < 20*time.Second {
		return
	}
	h.key, h.pos, h.playing, h.sent = string(k), pos, st.Active && !st.Paused, now
	b, _ := json.Marshal(st)
	h.last = sse("state", b)
	h.broadcast(h.last, "")
}

// command sends a speaker command to that device's streams.
func (h *hub) command(device string, c Cmd) {
	b, _ := json.Marshal(c)
	h.mu.Lock()
	h.broadcast(sse("cmd", b), device)
	h.mu.Unlock()
}

// broadcast never blocks: a page that fell behind catches up on the next
// state, and speakers fetch missed commands with their next report.
func (h *hub) broadcast(msg []byte, device string) {
	for c := range h.clients {
		if device != "" && c.device != device {
			continue
		}
		select {
		case c.ch <- msg:
		default:
		}
	}
}

// live reports which paired browsers have a page open.
func (h *hub) live() map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]bool{}
	for c := range h.clients {
		out[c.pair] = true
	}
	return out
}

// drop cuts off a paired browser's pages ("" cuts off all).
func (h *hub) drop(pair string) {
	h.mu.Lock()
	for c := range h.clients {
		if pair == "" || c.pair == pair {
			close(c.ch)
			delete(h.clients, c)
		}
	}
	h.mu.Unlock()
}

func (h *hub) closeAll() {
	h.mu.Lock()
	for c := range h.clients {
		close(c.ch)
		delete(h.clients, c)
	}
	h.last = nil
	h.mu.Unlock()
}

func sse(event string, data []byte) []byte {
	return append(append([]byte("event: "+event+"\ndata: "), data...), '\n', '\n')
}

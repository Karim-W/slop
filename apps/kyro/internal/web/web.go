// Package web serves kyro to browsers on the local network: the same
// player as the terminal (zen mode, cover colours and all), working as a
// remote for whatever plays now, and as a speaker. A TV can take over the
// song from the Mac, a phone can drive the TV, or play through its own
// Bluetooth speaker.
//
// Only paired browsers get in (see pair.go). Songs and covers are served
// by track ID, never by path, and only for tracks kyro already knows.
// Nothing leaves the network: the page loads no outside scripts or fonts.
package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/art"
	"github.com/karim-w/slop/apps/kyro/internal/dyntheme"
	"github.com/karim-w/slop/apps/kyro/internal/engine"
	"github.com/karim-w/slop/apps/kyro/internal/ipc"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/apps/kyro/internal/playlist"
	"github.com/karim-w/slop/apps/kyro/internal/ratings"
	"github.com/karim-w/slop/pkg/domain/theme"
)

// Port is where kyro listens unless told otherwise: "kyro" typed on a
// phone keypad (5-9-7-6).
const Port = 5976

//go:embed static
var static embed.FS

// Deps are what the server works with.
type Deps struct {
	Library   *library.Library
	Playlists playlist.Store
	Ratings   *ratings.Store
	Router    *engine.Router
	// Do runs a control command on the player, like the control socket.
	Do       func(ipc.Request) ipc.Response
	Mpv      string // mpv, to convert formats a browser can't play
	Dir      string // where devices.json lives
	CacheDir string // converted songs
	Name     string // this machine, as pages show it
	Host     string // "" listens on every network; tests use 127.0.0.1
	Log      *slog.Logger
}

// Server is the web player.
type Server struct {
	d     Deps
	pair  *pairing
	hub   *hub
	media *media

	mu      sync.Mutex
	srv     *http.Server
	port    int
	devices map[string]*Device
	queue   []library.Track
	qrev    string
	stop    chan struct{}
	covers  map[string]*coverLook // by cover key; nil: no art
	busy    map[string]bool
}

// New prepares a server; Start makes it listen.
func New(d Deps) *Server {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Server{d: d, pair: &pairing{path: filepath.Join(d.Dir, "devices.json")}, hub: newHub(),
		media: newMedia(d.Library, d.Mpv, d.CacheDir), devices: map[string]*Device{},
		covers: map[string]*coverLook{}, busy: map[string]bool{}}
}

// Start listens on every network interface at port.
func (s *Server) Start(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil {
		return nil
	}
	if port <= 0 {
		port = Port
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(s.d.Host, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("can't serve on port %d: %w", port, err)
	}
	s.port, s.stop = port, make(chan struct{})
	s.srv = &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	go s.srv.Serve(ln)
	go s.janitor(s.stop)
	go s.media.prune(2 << 30)
	return nil
}

// Stop closes the server and drops every browser speaker.
func (s *Server) Stop() error {
	s.mu.Lock()
	srv, devs := s.srv, s.devices
	if srv == nil {
		s.mu.Unlock()
		return nil
	}
	close(s.stop)
	s.srv, s.devices = nil, map[string]*Device{}
	s.mu.Unlock()
	s.pair.Close()
	s.pair.Flush()
	for id := range devs {
		s.d.Router.Remove(id)
	}
	s.hub.closeAll()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return srv.Close()
	}
	return nil
}

// Running reports whether the server listens.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.srv != nil
}

// URLs are the addresses to open on another device, the most reliable
// (an IP address) first.
func (s *Server) URLs() []string {
	s.mu.Lock()
	port := s.port
	s.mu.Unlock()
	var out []string
	// The .local name first: it stays the same when the network hands out
	// a new address, so a phone's pairing and home-screen icon keep working.
	if h := localName(); h != "" {
		out = append(out, fmt.Sprintf("http://%s:%d", h, port))
	}
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() {
				out = append(out, fmt.Sprintf("http://%s:%d", n.IP, port))
			}
		}
	}
	return out
}

// localName is this computer's Bonjour name, which phones on the same
// network resolve.
func localName() string {
	h := ""
	if runtime.GOOS == "darwin" {
		if b, err := exec.Command("scutil", "--get", "LocalHostName").Output(); err == nil {
			h = strings.TrimSpace(string(b))
		}
	}
	if h == "" {
		h, _ = os.Hostname()
	}
	if h == "" {
		return ""
	}
	h = strings.ToLower(strings.TrimSuffix(h, ".local"))
	if strings.Contains(h, ".") {
		return h
	}
	return h + ".local"
}

// OpenPairing starts letting browsers pair and returns the code.
func (s *Server) OpenPairing() string { return s.pair.Open() }

// ClosePairing stops new browsers pairing.
func (s *Server) ClosePairing() { s.pair.Close() }

// PairingCode is the code while pairing is open, else "".
func (s *Server) PairingCode() string { return s.pair.Code() }

// Paired lists the browsers that may connect, most recently seen first.
func (s *Server) Paired() []Paired {
	live := s.hub.live()
	out := s.pair.List()
	for i := range out {
		out[i].Live = live[out[i].ID]
	}
	return out
}

// Revoke unpairs one browser right away: its open pages are cut off and
// it stops being a speaker. It needs a new code to come back.
func (s *Server) Revoke(id string) error {
	if err := s.pair.Revoke(id); err != nil {
		return err
	}
	s.hub.drop(id)
	s.mu.Lock()
	var gone []string
	for did, d := range s.devices {
		if id == "" || d.pair == id {
			gone = append(gone, did)
			delete(s.devices, did)
		}
	}
	s.mu.Unlock()
	for _, did := range gone {
		s.d.Router.Remove(did)
	}
	return nil
}

// Forget unpairs every browser.
func (s *Server) Forget() error { return s.Revoke("") }

// Publish shows st on every page. queue is the player's queue, sent to
// pages when st.QueueRev changes.
func (s *Server) Publish(st State, queue func() []library.Track) {
	if !s.Running() {
		return
	}
	s.mu.Lock()
	if st.QueueRev != s.qrev {
		s.qrev, s.queue = st.QueueRev, slices.Clone(queue())
		s.media.remember(s.queue)
	}
	if st.Track != nil {
		if l := s.coverTheme(*st.Track, st.Theme); l != nil {
			st.Cover, st.Field = &l.theme, l.field
		}
	}
	s.mu.Unlock()
	s.hub.publish(st)
}

// coverLook is what a cover gives pages: its theme and the field of its
// colours that zen paints the screen with.
type coverLook struct {
	theme theme.Theme
	field []string
}

// coverTheme is the look of a track's cover, worked out once in the
// background (the next publish carries it). s.mu is held.
func (s *Server) coverTheme(t Track, base theme.Theme) *coverLook {
	if th, ok := s.covers[t.Art]; ok {
		return th
	}
	if s.busy[t.Art] {
		return nil
	}
	s.busy[t.Art] = true
	go func() {
		var th *coverLook
		if lt, ok := s.media.track(t.ID); ok {
			if data, _, ok := art.Source(lt); ok {
				if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
					c := dyntheme.From(img, base)
					th = &coverLook{c, dyntheme.Field(img, c)}
				}
			}
		}
		s.mu.Lock()
		if len(s.covers) > 256 {
			clear(s.covers)
		}
		s.covers[t.Art] = th
		delete(s.busy, t.Art)
		s.mu.Unlock()
	}()
	return nil
}

// janitor drops speakers whose page went away. A playing phone in a
// pocket may not report for a while, so the playing one gets longer.
func (s *Server) janitor(stop chan struct{}) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		active := s.d.Router.Active()
		s.mu.Lock()
		var gone []string
		for id, d := range s.devices {
			grace := 30 * time.Second
			if id == active {
				grace = 3 * time.Minute
			}
			if d.gone(grace) {
				gone = append(gone, id)
				delete(s.devices, id)
			}
		}
		s.mu.Unlock()
		for _, id := range gone {
			s.d.Router.Remove(id)
		}
	}
}

// ─── HTTP ───────────────────────────────────────────────────────────────────

const cookieName = "kyro_session"

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	files, _ := fs.Sub(static, "static")
	mux.Handle("GET /", http.FileServerFS(files))
	mux.HandleFunc("GET /api/hello", s.hello)
	mux.HandleFunc("POST /api/pair", s.pairHandler)
	mux.HandleFunc("POST /api/resume", s.resume)

	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			id, ok := s.who(r)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not paired"})
				return
			}
			h(w, r.WithContext(context.WithValue(r.Context(), pairKey{}, id)))
		}
	}
	mux.HandleFunc("GET /api/events", auth(s.events))
	mux.HandleFunc("GET /api/library", auth(s.library))
	mux.HandleFunc("GET /api/queue", auth(s.queueHandler))
	mux.HandleFunc("GET /api/playlists", auth(s.playlists))
	mux.HandleFunc("GET /api/playlists/{name}", auth(s.playlistHandler))
	mux.HandleFunc("GET /api/art/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		size, _ := strconv.Atoi(r.URL.Query().Get("s"))
		s.media.cover(w, r, r.PathValue("id"), size)
	}))
	mux.HandleFunc("GET /api/audio/{id}", auth(s.audio))
	mux.HandleFunc("POST /api/cmd", auth(s.cmd))
	mux.HandleFunc("POST /api/devices", auth(s.register))
	mux.HandleFunc("POST /api/devices/{id}/reports", auth(s.reports))
	mux.HandleFunc("DELETE /api/devices/{id}", auth(s.unregister))
	return guard(mux)
}

// guard sets strict headers and turns away cross-site writes: every
// change must carry X-Kyro, which a page on another site can't add
// without a CORS preflight kyro never answers.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; media-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-cache") // a home-screen app picks up a new kyro
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Kyro") != "1" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "missing X-Kyro header"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

type pairKey struct{}

// who is the paired browser behind a request.
func (s *Server) who(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", false
	}
	return s.pair.Check(c.Value)
}

func pairOf(r *http.Request) string { id, _ := r.Context().Value(pairKey{}).(string); return id }

func setKey(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: 400 * 24 * 3600})
}

// hello tells a page whether it is paired and what this machine is called.
func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	_, ok := s.who(r)
	writeJSON(w, http.StatusOK, map[string]any{"paired": ok, "server": s.d.Name})
}

// resume brings back a cookie the browser dropped, from the key the page
// kept: pairing is once and for all, until kyro revokes it.
func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	var req struct{ Key string }
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if _, ok := s.pair.Check(req.Key); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not paired"})
		return
	}
	setKey(w, req.Key)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) pairHandler(w http.ResponseWriter, r *http.Request) {
	var req struct{ Code, Name string }
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	token, err := s.pair.Pair(strings.TrimSpace(req.Code), clean(req.Name, "a browser"))
	if err != nil {
		status := http.StatusForbidden
		if !errors.Is(err, ErrWrongCode) && !errors.Is(err, ErrPairingClosed) {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	setKey(w, token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "key": token})
}

// events is the live stream: state for every page, and commands for the
// page's speaker.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	id := r.URL.Query().Get("device")
	s.mu.Lock()
	dev := s.devices[id]
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	c := s.hub.join(id, pairOf(r))
	defer s.hub.leave(c)
	if dev != nil {
		dev.touch(1)
		defer dev.touch(-1)
		for _, cmd := range dev.pending(0) {
			b, _ := json.Marshal(cmd)
			w.Write(sse("cmd", b))
		}
	}
	w.Write([]byte("retry: 2000\n\n"))
	fl.Flush()
	beat := time.NewTicker(15 * time.Second)
	defer beat.Stop()
	for {
		select {
		case msg, ok := <-c.ch:
			if !ok {
				return
			}
			if _, err := w.Write(msg); err != nil {
				return
			}
		case <-beat.C:
			if _, err := w.Write([]byte(": beat\n\n")); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
		fl.Flush()
	}
}

func (s *Server) tracks(ts []library.Track) []Track {
	out := make([]Track, len(ts))
	for i, t := range ts {
		out[i] = TrackOf(t, s.d.Ratings.Get(t.Path))
	}
	return out
}

func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tracks": s.tracks(s.d.Library.All())})
}

func (s *Server) queueHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	q, rev := s.queue, s.qrev
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"rev": rev, "tracks": s.tracks(q)})
}

func (s *Server) playlists(w http.ResponseWriter, r *http.Request) {
	ps, err := s.d.Playlists.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	type item struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	out := make([]item, 0, len(ps))
	for _, p := range ps {
		out = append(out, item{p.Name, len(p.Entries)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) playlistHandler(w http.ResponseWriter, r *http.Request) {
	p, err := s.d.Playlists.Get(r.PathValue("name"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such playlist"})
		return
	}
	ts := make([]library.Track, 0, len(p.Entries))
	for _, e := range p.Entries {
		t, ok := s.d.Library.ByPath(e.Path)
		if !ok {
			t = library.Track{ID: library.IDFor(e.Path), Path: e.Path, Title: e.Title, Duration: e.Duration, Missing: true}
			if t.Title == "" {
				t.Title = strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path))
			}
			if _, err := os.Stat(e.Path); err == nil {
				t.Missing = false
			}
		}
		ts = append(ts, t)
	}
	s.media.remember(ts)
	writeJSON(w, http.StatusOK, map[string]any{"name": p.Name, "tracks": s.tracks(ts)})
}

func (s *Server) audio(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	dev := s.devices[r.URL.Query().Get("d")]
	s.mu.Unlock()
	formats, af := map[string]bool{"mp3": true, "aac": true, "wav": true}, ""
	if dev != nil {
		formats, af = dev.formats, dev.filter()
	}
	s.media.audio(w, r, r.PathValue("id"), formats, af)
}

// commands pages may send; the rest of the control socket stays local.
var pageCommands = map[string]bool{
	"status": true, "toggle": true, "play": true, "pause": true, "next": true, "prev": true, "stop": true,
	"seek": true, "volume": true, "shuffle": true, "repeat": true, "love": true, "rate": true, "sound": true,
	"dynamic": true, "output": true, "jump": true, "play-tracks": true, "play-playlist": true,
	"enqueue": true, "play-next": true,
}

func (s *Server) cmd(w http.ResponseWriter, r *http.Request) {
	var req ipc.Request
	if err := readJSON(r, &req); err != nil || !pageCommands[req.Cmd] {
		writeJSON(w, http.StatusBadRequest, ipc.Response{Error: "unknown command"})
		return
	}
	resp := s.d.Do(req)
	status := http.StatusOK
	if !resp.OK {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, ipc.Response{OK: resp.OK, Error: resp.Error})
}

var deviceID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// register makes this page a speaker kyro can play through.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Formats []string `json:"formats"`
	}
	if err := readJSON(r, &req); err != nil || !deviceID.MatchString(req.ID) || req.ID == "local" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad device"})
		return
	}
	name := clean(req.Name, "Browser")
	s.mu.Lock()
	d, ok := s.devices[req.ID]
	if !ok {
		d = newDevice(s, req.ID, name, req.Formats)
		d.pair = pairOf(r)
		s.devices[req.ID] = d
		go d.tick()
	} else {
		d.name = name
		d.touch(0)
	}
	s.mu.Unlock()
	s.d.Router.Add(req.ID, name, d)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) device(id string) *Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.devices[id]
}

// reports takes a speaker's news and answers with its pending commands.
func (s *Server) reports(w http.ResponseWriter, r *http.Request) {
	d := s.device(r.PathValue("id"))
	if d == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not a speaker"})
		return
	}
	var req struct {
		Ack     int64    `json:"ack"`
		Reports []report `json:"reports"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	d.apply(req.Reports)
	writeJSON(w, http.StatusOK, map[string]any{"cmds": d.pending(req.Ack)})
}

func (s *Server) unregister(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	_, ok := s.devices[id]
	delete(s.devices, id)
	s.mu.Unlock()
	if ok {
		s.d.Router.Remove(id)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// clean keeps a name short and printable.
func clean(s, def string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > 40 {
		s = string(r[:40])
	}
	if s == "" {
		return def
	}
	return s
}

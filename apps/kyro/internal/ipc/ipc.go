// Package ipc is kyro's control socket: one JSON object per line over a
// Unix socket at ~/.kyro/kyro.sock.
//
//	→ {"cmd":"status"}
//	← {"ok":true,"state":{"event":"track","playing":true,"track":{…},…}}
//	→ {"cmd":"subscribe"}
//	← one state line now, then one per playback event, until disconnect
//
// Commands: status, subscribe, toggle, play, pause, next, prev, stop,
// seek <seconds | +N | -N>, volume <0-100 | +N | -N>, shuffle, repeat,
// love, rate <0-5>, sound [MODE], dynamic, output <ID>, jump (index),
// play-tracks (ids, index, arg "album:NAME"…), play-playlist <NAME>
// (index), enqueue (ids) and play-next (ids).
// `kyro ctl …` and `kyro status` are thin clients of this socket, so window
// manager key bindings and status bars can drive and show kyro.
package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/hooks"
)

// Request is one command.
type Request struct {
	Cmd string `json:"cmd"`
	Arg string `json:"arg,omitempty"`
	// IDs and Index pick tracks for play-tracks, enqueue, play-next and
	// jump (see the package doc).
	IDs   []string `json:"ids,omitempty"`
	Index int      `json:"index,omitempty"`
}

// Response answers a request.
type Response struct {
	OK    bool         `json:"ok"`
	Error string       `json:"error,omitempty"`
	State *hooks.Event `json:"state,omitempty"`
}

// ErrRunning means another kyro already owns the socket.
var ErrRunning = errors.New("another kyro is already running")

// Server accepts clients and broadcasts events to subscribers.
type Server struct {
	ln      net.Listener
	path    string
	handle  func(Request) Response
	mu      sync.Mutex
	subs    map[chan hooks.Event]struct{}
	closing bool
}

// Listen opens the socket. A stale socket left by a crash is replaced;
// a live one means another instance is running.
func Listen(path string, handle func(Request) Response) (*Server, error) {
	if _, err := os.Stat(path); err == nil {
		if c, err := net.DialTimeout("unix", path, 300*time.Millisecond); err == nil {
			c.Close()
			return nil, ErrRunning
		}
		os.Remove(path)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	os.Chmod(path, 0o600) // only this user may control the player
	s := &Server{ln: ln, path: path, handle: handle, subs: map[chan hooks.Event]struct{}{}}
	go s.accept()
	return s, nil
}

func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(c)
	}
}

func (s *Server) serve(c net.Conn) {
	defer c.Close()
	sc := bufio.NewScanner(c)
	enc := json.NewEncoder(c)
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			enc.Encode(Response{Error: "bad request: " + err.Error()})
			continue
		}
		if req.Cmd == "subscribe" {
			s.subscribe(c, enc)
			return
		}
		c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if enc.Encode(s.handle(req)) != nil {
			return
		}
	}
}

func (s *Server) subscribe(c net.Conn, enc *json.Encoder) {
	ch := make(chan hooks.Event, 16)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()
	// Notice the client hanging up even when nothing is playing.
	gone := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		for {
			if _, err := c.Read(buf); err != nil {
				close(gone)
				return
			}
		}
	}()
	if r := s.handle(Request{Cmd: "status"}); r.State != nil {
		enc.Encode(r.State)
	}
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if enc.Encode(e) != nil {
				return
			}
		case <-gone:
			return
		}
	}
}

// Broadcast sends e to every subscriber. Slow subscribers miss events
// rather than stall the player.
func (s *Server) Broadcast(e hooks.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Close stops listening and removes the socket.
func (s *Server) Close() error {
	s.mu.Lock()
	for ch := range s.subs {
		close(ch)
		delete(s.subs, ch)
	}
	s.mu.Unlock()
	err := s.ln.Close()
	os.Remove(s.path)
	return err
}

// ─── Client ─────────────────────────────────────────────────────────────────

// Call sends one request to a running kyro.
func Call(path string, req Request) (Response, error) {
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return Response{}, errors.New("kyro is not running")
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK && resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

// Subscribe streams events from a running kyro to fn until it disconnects
// or fn returns false.
func Subscribe(path string, fn func(hooks.Event) bool) error {
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return errors.New("kyro is not running")
	}
	defer c.Close()
	if err := json.NewEncoder(c).Encode(Request{Cmd: "subscribe"}); err != nil {
		return err
	}
	dec := json.NewDecoder(c)
	for {
		var e hooks.Event
		if err := dec.Decode(&e); err != nil {
			return nil
		}
		if !fn(e) {
			return nil
		}
	}
}

package engine

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/home"
	"github.com/karim-w/slop/apps/kyro/internal/sound"
)

// Mpv drives an mpv process over its JSON IPC.
//
// Safety, since this touches the sound hardware:
//   - Output goes through the OS mixer (CoreAudio, PipeWire/PulseAudio,
//     WASAPI) in shared mode, exactly like any media player. Exclusive mode
//     and S/PDIF passthrough are only used when the config asks for them.
//   - Surround uses "auto-safe": extra channels are only sent when the device
//     reports it accepts them; otherwise mpv downmixes.
//   - Volume is capped at 100 (no software amplification, no clipping).
//   - mpv is told to ignore the user's own mpv.conf and scripts, so nothing
//     unexpected is enabled.
//   - The IPC channel is a socket pair inherited by mpv. If kyro exits or
//     crashes, the pair closes and mpv quits: no orphaned audio.
type Mpv struct {
	cmd    *exec.Cmd
	conn   net.Conn
	events chan Event
	reqID  atomic.Int64
	wmu    sync.Mutex
	done   chan struct{}

	mu         sync.Mutex
	path       string // file mpv reports as current
	next       string
	lastPos    time.Time
	pendingPos *time.Duration // newest position held back by the throttle
	posTimer   *time.Timer
	closed     bool
	exitErr    error
	stderrTail *tailBuffer
}

// MpvPath finds mpv: the configured path, then $PATH, then common install
// locations that GUI-launched shells may lack.
func MpvPath(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	if p, err := exec.LookPath("mpv"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/opt/homebrew/bin/mpv", "/usr/local/bin/mpv", "/usr/bin/mpv", "/snap/bin/mpv"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("mpv not found: kyro uses mpv to play audio. Install it with `brew install mpv` (macOS) or your package manager, or set audio.mpv in ~/.kyro/config.json")
}

// Args builds mpv's command line from the audio settings.
func Args(a home.Audio, volume int) []string {
	args := []string{
		"--no-config", // ignore ~/.config/mpv: predictable, nothing surprising enabled
		"--idle=yes",
		"--no-video",
		"--no-terminal",
		"--audio-display=no",
		"--load-scripts=no",
		"--osc=no",
		"--input-default-bindings=no",
		"--gapless-audio=weak",
		"--prefetch-playlist=yes",
		"--volume-max=100",
		fmt.Sprintf("--volume=%d", min(max(volume, 0), 100)),
		"--audio-channels=" + orDefault(a.Channels, "auto-safe"),
		"--replaygain=" + orDefault(a.ReplayGain, "no"),
		"--audio-pitch-correction=no",
		// Downmixing surround to fewer speakers keeps the mix's balance
		// without clipping.
		"--audio-normalize-downmix=yes",
		// Media keys and Now Playing: mpv registers with the system (macOS
		// Control Center, keyboard media keys, headphone buttons) and kyro
		// binds those keys to its own queue (see mediaKeys).
		"--media-controls=" + map[bool]string{true: "yes", false: "no"}[a.MediaKeys != "off"],
	}
	if af := sound.Filter(a.Sound); af != "" {
		args = append(args, "--af="+af)
	}
	if a.Device != "" {
		args = append(args, "--audio-device="+a.Device)
	}
	if a.Exclusive {
		args = append(args, "--audio-exclusive=yes")
	}
	if len(a.Passthrough) > 0 {
		args = append(args, "--audio-spdif="+strings.Join(a.Passthrough, ","))
	}
	return append(args, a.Extra...)
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// StartMpv launches mpv with the given settings.
func StartMpv(a home.Audio, volume int) (*Mpv, error) {
	bin, err := MpvPath(a.Mpv)
	if err != nil {
		return nil, err
	}
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}
	ours := os.NewFile(uintptr(fds[0]), "kyro-ipc")
	theirs := os.NewFile(uintptr(fds[1]), "mpv-ipc")
	defer theirs.Close()

	args := append(Args(a, volume), "--input-ipc-client=fd://3") // ExtraFiles[0] is fd 3
	cmd := exec.Command(bin, args...)
	cmd.ExtraFiles = []*os.File{theirs}
	tail := &tailBuffer{}
	cmd.Stderr = tail
	if err := cmd.Start(); err != nil {
		ours.Close()
		return nil, fmt.Errorf("starting mpv: %w", err)
	}
	conn, err := net.FileConn(ours)
	ours.Close()
	if err != nil {
		cmd.Process.Kill()
		return nil, err
	}
	m := &Mpv{cmd: cmd, conn: conn, events: make(chan Event, 64), done: make(chan struct{}), stderrTail: tail}
	go m.read()
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		m.exitErr = err
		m.mu.Unlock()
		conn.Close()
	}()
	for i, p := range []string{"time-pos", "duration", "pause", "path", "audio-params", "audio-out-params"} {
		if err := m.send(map[string]any{"command": []any{"observe_property", i + 1, p}}); err != nil {
			m.Close()
			return nil, err
		}
	}
	// Media keys arrive in mpv as these key names. Each one becomes a
	// message to kyro instead of acting on mpv's own two-item playlist.
	for key, cmd := range mediaKeys {
		if err := m.command("keybind", key, "script-message kyro-remote "+cmd); err != nil {
			m.Close()
			return nil, err
		}
	}
	return m, nil
}

// mediaKeys maps mpv's media key names to Remote commands. As in mpv's
// own input.conf, PLAY, PAUSE and PLAYPAUSE all toggle: the keyboard's
// play/pause key and Control Center's toggle arrive as PLAY. Only
// PLAYONLY and PAUSEONLY mean one thing.
var mediaKeys = map[string]string{
	"PLAY": RemoteToggle, "PAUSE": RemoteToggle, "PLAYPAUSE": RemoteToggle,
	"PLAYONLY": RemotePlay, "PAUSEONLY": RemotePause, "STOP": RemoteStop,
	"NEXT": RemoteNext, "PREV": RemotePrev, "FORWARD": RemoteForward, "REWIND": RemoteRewind,
}

func (m *Mpv) send(v map[string]any) error {
	v["request_id"] = m.reqID.Add(1)
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	m.wmu.Lock()
	defer m.wmu.Unlock()
	m.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err = m.conn.Write(append(b, '\n'))
	return err
}

func (m *Mpv) command(args ...any) error { return m.send(map[string]any{"command": args}) }

func (m *Mpv) Play(path string) error {
	m.mu.Lock()
	m.next = ""
	m.mu.Unlock()
	if err := m.command("loadfile", path, "replace"); err != nil {
		return err
	}
	return m.command("set_property", "pause", false)
}

// PlayFrom starts path at a position, paused or playing, for a hand-over
// from another output.
func (m *Mpv) PlayFrom(path string, at time.Duration, paused bool) error {
	m.mu.Lock()
	m.next = ""
	m.mu.Unlock()
	if err := m.command("set_property", "pause", paused); err != nil {
		return err
	}
	return m.command("loadfile", path, "replace", -1, fmt.Sprintf("start=%.3f", max(at.Seconds(), 0)))
}

func (m *Mpv) Next(path string) error {
	m.mu.Lock()
	m.next = path
	m.mu.Unlock()
	// Keep mpv's playlist to [current, next]: drop everything else first.
	if err := m.command("playlist-clear"); err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	return m.command("loadfile", path, "append")
}

func (m *Mpv) SetPaused(p bool) error { return m.command("set_property", "pause", p) }

func (m *Mpv) Seek(d time.Duration) error {
	return m.command("seek", max(d.Seconds(), 0), "absolute+exact")
}

func (m *Mpv) SetVolume(v int) error {
	return m.command("set_property", "volume", min(max(v, 0), 100))
}

// SetFilter swaps mpv's audio filters while playing.
func (m *Mpv) SetFilter(af string) error { return m.command("set_property", "af", af) }

func (m *Mpv) Stop() error {
	m.mu.Lock()
	m.next = ""
	m.mu.Unlock()
	return m.command("stop")
}

// Events delivers what happens. The channel stays open; Exited is last.
func (m *Mpv) Events() <-chan Event { return m.events }

// Close stops playback and shuts mpv down.
func (m *Mpv) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()
	m.command("quit")
	select {
	case <-m.done:
	case <-time.After(2 * time.Second):
		m.cmd.Process.Kill()
	}
	m.conn.Close()
	return nil
}

// emit delivers an event, dropping position updates if the app is behind.
func (m *Mpv) emit(e Event) {
	select {
	case <-m.done:
		return // reader finished; nobody listens any more
	default:
	}
	if e.Kind == Position {
		select {
		case m.events <- e:
		default:
		}
		return
	}
	select {
	case m.events <- e:
	case <-m.done:
	}
}

type mpvMsg struct {
	Event   string          `json:"event"`
	Name    string          `json:"name"`
	Data    json.RawMessage `json:"data"`
	Reason  string          `json:"reason"`
	FileErr string          `json:"file_error"`
	Error   string          `json:"error"`
	Args    []string        `json:"args"`
}

func (m *Mpv) read() {
	// m.events is never closed (a throttled position may still be in
	// flight); Exited is always the last event, and done marks the end.
	sc := bufio.NewScanner(m.conn)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var msg mpvMsg
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		m.handle(msg)
	}
	m.mu.Lock()
	closed, err := m.closed, m.exitErr
	m.mu.Unlock()
	if !closed {
		if tail := m.stderrTail.String(); tail != "" {
			err = fmt.Errorf("mpv exited: %s", tail)
		} else if err == nil {
			err = errors.New("mpv exited")
		}
	}
	m.emit(Event{Kind: Exited, Err: err})
	close(m.done)
}

func (m *Mpv) handle(msg mpvMsg) {
	switch msg.Event {
	case "property-change":
		m.property(msg.Name, msg.Data)
	case "client-message":
		if len(msg.Args) == 2 && msg.Args[0] == "kyro-remote" {
			m.emit(Event{Kind: Remote, Command: msg.Args[1]})
		}
	case "end-file":
		r := EndReason(msg.Reason)
		var err error
		switch msg.Reason {
		case "eof":
			r = EOF
		case "error":
			r, err = Error, errors.New(orDefault(msg.FileErr, "can't play this file"))
		case "stop":
			r = Stopped
		case "redirect", "quit":
			r = Replaced
		}
		m.mu.Lock()
		path, cont := m.path, m.next != "" && r == EOF
		m.mu.Unlock()
		m.emit(Event{Kind: Ended, Path: path, Reason: r, Err: err, Continues: cont})
	}
}

func (m *Mpv) property(name string, data json.RawMessage) {
	switch name {
	case "path":
		var p string
		if json.Unmarshal(data, &p) != nil || p == "" {
			return
		}
		m.mu.Lock()
		m.path = p
		if p == m.next {
			m.next = ""
		}
		m.mu.Unlock()
		m.emit(Event{Kind: Started, Path: p})
	case "time-pos":
		var v float64
		if json.Unmarshal(data, &v) != nil {
			return
		}
		m.position(secs(v))
	case "duration":
		var s float64
		if json.Unmarshal(data, &s) == nil && s > 0 {
			m.emit(Event{Kind: Duration, Duration: secs(s)})
		}
	case "pause":
		var p bool
		if json.Unmarshal(data, &p) == nil {
			m.emit(Event{Kind: Paused, Paused: p})
		}
	case "audio-params", "audio-out-params":
		var p struct {
			SampleRate int    `json:"samplerate"`
			Channels   int    `json:"channel-count"`
			Layout     string `json:"hr-channels"`
			Format     string `json:"format"`
		}
		if json.Unmarshal(data, &p) != nil || p.SampleRate == 0 {
			return
		}
		ap := AudioParams{SampleRate: p.SampleRate, Channels: p.Channels, Layout: p.Layout, Format: p.Format}
		if name == "audio-params" {
			m.emit(Event{Kind: Params, Source: ap})
		} else {
			m.emit(Event{Kind: Params, Output: ap})
		}
	}
}

// position forwards at most five updates a second, and always the latest
// one: an update held back by the throttle is sent when the window ends, so
// a seek while paused still lands on the right time.
func (m *Mpv) position(p time.Duration) {
	const window = 200 * time.Millisecond
	m.mu.Lock()
	since := time.Since(m.lastPos)
	if since >= window {
		m.lastPos, m.pendingPos = time.Now(), nil
		m.mu.Unlock()
		m.emit(Event{Kind: Position, Pos: p})
		return
	}
	m.pendingPos = &p
	if m.posTimer == nil {
		m.posTimer = time.AfterFunc(window-since, func() {
			m.mu.Lock()
			pending := m.pendingPos
			m.pendingPos, m.posTimer, m.lastPos = nil, nil, time.Now()
			closed := m.closed
			m.mu.Unlock()
			if pending != nil && !closed {
				m.emit(Event{Kind: Position, Pos: *pending})
			}
		})
	}
	m.mu.Unlock()
}

func secs(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// tailBuffer keeps the last few hundred bytes of mpv's stderr for errors.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 600 {
		t.buf = t.buf[len(t.buf)-600:]
	}
	t.mu.Unlock()
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// Devices lists mpv's audio output devices as name → description.
func Devices(a home.Audio) ([][2]string, error) {
	bin, err := MpvPath(a.Mpv)
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(bin, "--no-config", "--audio-device=help").Output()
	if err != nil {
		return nil, err
	}
	var devs [][2]string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "'") {
			continue
		}
		name, desc, _ := strings.Cut(line[1:], "' (")
		devs = append(devs, [2]string{name, strings.TrimSuffix(desc, ")")})
	}
	return devs, nil
}

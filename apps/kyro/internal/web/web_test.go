package web

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/engine"
	"github.com/karim-w/slop/apps/kyro/internal/ipc"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/apps/kyro/internal/playlist"
	"github.com/karim-w/slop/apps/kyro/internal/ratings"
	"github.com/karim-w/slop/apps/kyro/internal/sound"
)

func writeWAV(t *testing.T, path string, d time.Duration) {
	t.Helper()
	const rate = 44100
	n := int(d.Seconds() * rate)
	data := make([]byte, 0, n*4)
	for i := 0; i < n; i++ {
		v := int16(math.Sin(2*math.Pi*440*float64(i)/rate) * 8000)
		data = binary.LittleEndian.AppendUint16(data, uint16(v))
		data = binary.LittleEndian.AppendUint16(data, uint16(v))
	}
	h := []byte("RIFF")
	h = binary.LittleEndian.AppendUint32(h, uint32(36+len(data)))
	h = append(h, "WAVEfmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint16(h, 2)
	h = binary.LittleEndian.AppendUint32(h, rate)
	h = binary.LittleEndian.AppendUint32(h, rate*4)
	h = binary.LittleEndian.AppendUint16(h, 4)
	h = binary.LittleEndian.AppendUint16(h, 16)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(data)))
	if err := os.WriteFile(path, append(h, data...), 0o644); err != nil {
		t.Fatal(err)
	}
}

type rig struct {
	s      *Server
	ts     *httptest.Server
	c      *http.Client
	track  library.Track
	router *engine.Router
	dir    string
	mu     sync.Mutex
	reqs   []ipc.Request
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	song := filepath.Join(dir, "music", "Artist - Song.wav")
	os.MkdirAll(filepath.Dir(song), 0o755)
	writeWAV(t, song, 3*time.Second)
	lib, _ := library.Open(filepath.Join(dir, "library.json"))
	tr, err := library.Read(song)
	if err != nil {
		t.Fatal(err)
	}
	lib.Update(tr)
	rs, _ := ratings.Open(filepath.Join(dir, "ratings.json"))
	r := &rig{track: tr, dir: dir, router: engine.NewRouter("local", "Mac", nil, 50, nil)}
	mpv, _ := engine.MpvPath("")
	r.s = New(Deps{
		Library: lib, Playlists: playlist.Store{Dir: filepath.Join(dir, "playlists")}, Ratings: rs, Router: r.router,
		Do: func(q ipc.Request) ipc.Response {
			r.mu.Lock()
			r.reqs = append(r.reqs, q)
			r.mu.Unlock()
			return ipc.Response{OK: true}
		},
		Mpv: mpv, Dir: filepath.Join(dir, "web"), CacheDir: filepath.Join(dir, "cache"), Name: "Test Mac",
	})
	r.s.srv = &http.Server{} // marks it running for Publish
	r.ts = httptest.NewServer(r.s.routes())
	t.Cleanup(r.ts.Close)
	jar, _ := cookiejar.New(nil)
	r.c = &http.Client{Jar: jar}
	return r
}

func (r *rig) do(t *testing.T, method, path string, body any) (*http.Response, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, r.ts.URL+path, rd)
	if method != http.MethodGet {
		req.Header.Set("X-Kyro", "1")
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (r *rig) pair(t *testing.T) {
	t.Helper()
	code := r.s.OpenPairing()
	if resp, body := r.do(t, "POST", "/api/pair", map[string]string{"code": code, "name": "phone"}); resp.StatusCode != 200 {
		t.Fatalf("pair: %d %s", resp.StatusCode, body)
	}
	r.s.ClosePairing()
}

func TestOnlyPairedBrowsersGetIn(t *testing.T) {
	r := newRig(t)
	if resp, _ := r.do(t, "GET", "/api/library", nil); resp.StatusCode != 401 {
		t.Fatalf("library without pairing: %d", resp.StatusCode)
	}
	if resp, _ := r.do(t, "GET", "/", nil); resp.StatusCode != 200 {
		t.Fatalf("the page itself loads: %d", resp.StatusCode)
	}
	// Changes need X-Kyro, which other sites can't send.
	resp, err := r.c.Post(r.ts.URL+"/api/pair", "application/json", strings.NewReader(`{"code":"000000"}`))
	if err != nil || resp.StatusCode != 403 {
		t.Fatalf("pair without X-Kyro: %v %v", resp.StatusCode, err)
	}
	// Closed until kyro's web screen opens it.
	if resp, body := r.do(t, "POST", "/api/pair", map[string]string{"code": "123456"}); resp.StatusCode != 403 || !strings.Contains(body, "closed") {
		t.Fatalf("pairing while closed: %d %s", resp.StatusCode, body)
	}
	code := r.s.OpenPairing()
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 1; i <= maxFails; i++ {
		resp, body := r.do(t, "POST", "/api/pair", map[string]string{"code": wrong})
		if resp.StatusCode != 403 {
			t.Fatalf("wrong code %d: %d", i, resp.StatusCode)
		}
		if i == maxFails && !strings.Contains(body, "closed") {
			t.Fatalf("pairing should close after %d wrong codes: %s", maxFails, body)
		}
	}
	if resp, _ := r.do(t, "POST", "/api/pair", map[string]string{"code": code}); resp.StatusCode != 403 {
		t.Fatal("the right code must not work once pairing closed")
	}

	r.pair(t)
	resp, body := r.do(t, "GET", "/api/library", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, r.track.ID) {
		t.Fatalf("library after pairing: %d %s", resp.StatusCode, body)
	}
	if strings.Contains(body, r.dir) || strings.Contains(body, "Song.wav") {
		t.Fatalf("file paths leak to the page: %s", body)
	}
	// Only a hash of the token is kept, in a private file.
	info, err := os.Stat(filepath.Join(r.dir, "web", "devices.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("devices.json: %v %v", info, err)
	}
	saved, _ := os.ReadFile(filepath.Join(r.dir, "web", "devices.json"))
	for _, c := range r.c.Jar.Cookies(mustURL(r.ts.URL)) {
		if strings.Contains(string(saved), c.Value) {
			t.Fatal("the token itself is stored")
		}
	}
	// The key the page keeps brings a dropped cookie back.
	var key string
	jar, _ := cookiejar.New(nil)
	r.c.Jar = jar
	code = r.s.OpenPairing()
	_, body = r.do(t, "POST", "/api/pair", map[string]string{"code": code, "name": "iPhone"})
	r.s.ClosePairing()
	var pr struct{ Key string }
	json.Unmarshal([]byte(body), &pr)
	key = pr.Key
	jar2, _ := cookiejar.New(nil)
	r.c.Jar = jar2
	if resp, _ := r.do(t, "GET", "/api/library", nil); resp.StatusCode != 401 {
		t.Fatal("no cookie should mean no entry")
	}
	if resp, _ := r.do(t, "POST", "/api/resume", map[string]string{"key": key}); resp.StatusCode != 200 {
		t.Fatalf("resume: %d", resp.StatusCode)
	}
	if resp, _ := r.do(t, "GET", "/api/library", nil); resp.StatusCode != 200 {
		t.Fatal("resume should bring the cookie back")
	}
	// Revoking one browser cuts it off and leaves the other paired.
	list := r.s.Paired()
	if len(list) != 2 {
		t.Fatalf("paired %+v", list)
	}
	var iphone string
	for _, p := range list {
		if p.Name == "iPhone" {
			iphone = p.ID
		}
	}
	if err := r.s.Revoke(iphone); err != nil {
		t.Fatal(err)
	}
	if resp, _ := r.do(t, "GET", "/api/library", nil); resp.StatusCode != 401 {
		t.Fatal("a revoked browser still gets in")
	}
	if resp, _ := r.do(t, "POST", "/api/resume", map[string]string{"key": key}); resp.StatusCode != 401 {
		t.Fatal("a revoked key must not resume")
	}
	if l := r.s.Paired(); len(l) != 1 || l[0].Name != "phone" {
		t.Fatalf("after revoke %+v", l)
	}
	r.c.Jar = jar
	if err := r.s.Forget(); err != nil {
		t.Fatal(err)
	}
	if resp, _ := r.do(t, "GET", "/api/library", nil); resp.StatusCode != 401 {
		t.Fatal("forgotten browsers still get in")
	}
}

func TestSongsAreServedByIDOnly(t *testing.T) {
	r := newRig(t)
	r.pair(t)
	resp, body := r.do(t, "GET", "/api/audio/"+r.track.ID, nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "audio/wav" || !strings.HasPrefix(body, "RIFF") {
		t.Fatalf("audio: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	req, _ := http.NewRequest("GET", r.ts.URL+"/api/audio/"+r.track.ID, nil)
	req.Header.Set("Range", "bytes=100-199")
	rr, err := r.c.Do(req)
	if err != nil || rr.StatusCode != 206 {
		t.Fatalf("seeking needs ranges: %v %v", rr.StatusCode, err)
	}
	rr.Body.Close()
	for _, p := range []string{"/api/audio/0123456789abcdef", "/api/audio/..%2F..%2Fetc%2Fpasswd", "/api/art/nope"} {
		if resp, _ := r.do(t, "GET", p, nil); resp.StatusCode != 404 {
			t.Fatalf("%s: %d", p, resp.StatusCode)
		}
	}
}

func TestBrowserSpeakerTakesTheSongOver(t *testing.T) {
	r := newRig(t)
	r.pair(t)
	if resp, body := r.do(t, "POST", "/api/devices", map[string]any{"id": "phone-123456", "name": "Phone", "formats": []string{"wav", "mp3"}}); resp.StatusCode != 200 {
		t.Fatalf("register: %d %s", resp.StatusCode, body)
	}
	outs := r.router.Outputs()
	if len(outs) != 2 || outs[1].Name != "Phone" {
		t.Fatalf("outputs %+v", outs)
	}
	r.router.Play(r.track.Path) // fails on the Mac (no mpv here), the router remembers it
	if err := r.router.Switch("phone-123456"); err != nil {
		t.Fatal(err)
	}
	var res struct{ Cmds []Cmd }
	_, body := r.do(t, "POST", "/api/devices/phone-123456/reports", map[string]any{"ack": 0})
	json.Unmarshal([]byte(body), &res)
	if len(res.Cmds) != 2 || res.Cmds[0].Op != "volume" || res.Cmds[0].Volume != 50 || res.Cmds[1].Op != "play" || res.Cmds[1].Track != r.track.ID {
		t.Fatalf("commands %s", body)
	}
	play := res.Cmds[1]
	if resp, _ := r.do(t, "GET", play.URL, nil); resp.StatusCode != 200 {
		t.Fatalf("the speaker can't fetch its song: %d", resp.StatusCode)
	}
	// Acknowledged commands aren't sent again.
	_, body = r.do(t, "POST", "/api/devices/phone-123456/reports", map[string]any{
		"ack": res.Cmds[len(res.Cmds)-1].Seq,
		"reports": []map[string]any{
			{"kind": "started", "track": r.track.ID}, // the hand-over, not a new play
			{"kind": "pos", "pos": 1.5},
			{"kind": "ended", "track": r.track.ID, "reason": "eof"},
		},
	})
	if strings.Contains(body, `"op":"play"`) {
		t.Fatalf("acknowledged command sent again: %s", body)
	}
	got := []engine.Event{}
	deadline := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case e := <-r.router.Events():
			got = append(got, e)
		case <-deadline:
			t.Fatalf("events %+v", got)
		}
	}
	if got[0].Kind != engine.Position || got[0].Pos != 1500*time.Millisecond || got[1].Kind != engine.Ended || got[1].Path != r.track.Path {
		t.Fatalf("events %+v", got)
	}

	r.do(t, "DELETE", "/api/devices/phone-123456", nil)
	if r.router.Active() != "local" || len(r.router.Outputs()) != 1 {
		t.Fatal("a speaker that leaves hands back to the Mac")
	}
}

func TestPagesOnlySendPlayerCommands(t *testing.T) {
	r := newRig(t)
	r.pair(t)
	if resp, _ := r.do(t, "POST", "/api/cmd", ipc.Request{Cmd: "subscribe"}); resp.StatusCode != 400 {
		t.Fatalf("subscribe from a page: %d", resp.StatusCode)
	}
	if resp, _ := r.do(t, "POST", "/api/cmd", ipc.Request{Cmd: "play-tracks", IDs: []string{r.track.ID}, Index: 0, Arg: "album:X"}); resp.StatusCode != 200 {
		t.Fatalf("play-tracks: %d", resp.StatusCode)
	}
	if len(r.reqs) != 1 || r.reqs[0].IDs[0] != r.track.ID || r.reqs[0].Arg != "album:X" {
		t.Fatalf("requests %+v", r.reqs)
	}
}

func TestStateStreamsToPages(t *testing.T) {
	r := newRig(t)
	r.pair(t)
	req, _ := http.NewRequest("GET", r.ts.URL+"/api/events", nil)
	resp, err := r.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	tr := TrackOf(r.track, ratings.Rating{Loved: true})
	r.s.Publish(State{Active: true, Track: &tr, Pos: 12, QueueRev: "q1"}, func() []library.Track { return []library.Track{r.track} })
	buf := make([]byte, 4096)
	var got string
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(got, `"loved":true`) && time.Now().Before(deadline) {
		n, err := resp.Body.Read(buf)
		got += string(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(got, "event: state") || !strings.Contains(got, `"pos":12`) {
		t.Fatalf("stream: %q", got)
	}
	_, body := r.do(t, "GET", "/api/queue", nil)
	if !strings.Contains(body, r.track.ID) {
		t.Fatalf("queue: %s", body)
	}
}

func TestConvertsWhatTheBrowserCantPlay(t *testing.T) {
	r := newRig(t)
	if r.s.d.Mpv == "" {
		t.Skip("mpv not installed")
	}
	r.pair(t)
	r.do(t, "POST", "/api/devices", map[string]any{"id": "tv-12345678", "name": "TV", "formats": []string{"flac", "mp3"}})
	resp, body := r.do(t, "GET", "/api/audio/"+r.track.ID+"?d=tv-12345678", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "audio/flac" || !strings.HasPrefix(body, "fLaC") {
		t.Fatalf("conversion: %d %s %q", resp.StatusCode, resp.Header.Get("Content-Type"), body[:min(len(body), 8)])
	}
	files, _ := os.ReadDir(filepath.Join(r.dir, "cache"))
	if len(files) != 1 {
		t.Fatalf("cache: %v", files)
	}
}

func mustURL(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}

func TestBrowserSpeakersGetTheSoundMode(t *testing.T) {
	r := newRig(t)
	if r.s.d.Mpv == "" {
		t.Skip("mpv not installed")
	}
	r.pair(t)
	r.do(t, "POST", "/api/devices", map[string]any{"id": "phone-12345678", "name": "iPhone", "formats": []string{"wav", "flac", "alac"}})
	r.router.SetFilter(sound.Filter("punch"))
	r.router.Play(r.track.Path)
	r.router.Switch("phone-12345678")
	_, body := r.do(t, "POST", "/api/devices/phone-12345678/reports", map[string]any{"ack": 0})
	if !strings.Contains(body, "sound mode") {
		t.Fatalf("the phone should be told it gets the sound mode: %s", body)
	}
	resp, audio := r.do(t, "GET", "/api/audio/"+r.track.ID+"?d=phone-12345678", nil)
	if resp.StatusCode != 200 || !strings.HasPrefix(audio, "fLaC") {
		t.Fatalf("sound mode copy: %d %q", resp.StatusCode, audio[:min(len(audio), 8)])
	}
	r.router.SetFilter("")
	if _, plain := r.do(t, "GET", "/api/audio/"+r.track.ID+"?d=phone-12345678", nil); !strings.HasPrefix(plain, "RIFF") {
		t.Fatal("pure sends the file untouched")
	}
}

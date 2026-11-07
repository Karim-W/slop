package engine

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/home"
)

// writeWAV writes a 24-bit PCM sine of the given length.
func writeWAV(t *testing.T, path string, rate, channels int, d time.Duration) {
	t.Helper()
	frames := int(d.Seconds() * float64(rate))
	data := make([]byte, 0, frames*channels*3)
	for i := 0; i < frames; i++ {
		v := int32(math.Sin(2*math.Pi*440*float64(i)/float64(rate)) * (1 << 22))
		for c := 0; c < channels; c++ {
			data = append(data, byte(v), byte(v>>8), byte(v>>16))
		}
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(data)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], uint16(channels))
	binary.LittleEndian.PutUint32(h[24:], uint32(rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(rate*channels*3))
	binary.LittleEndian.PutUint16(h[32:], uint16(channels*3))
	binary.LittleEndian.PutUint16(h[34:], 24)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(data)))
	if err := os.WriteFile(path, append(h, data...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func startTestMpv(t *testing.T) *Mpv {
	t.Helper()
	if _, err := MpvPath(""); err != nil {
		t.Skip("mpv not installed")
	}
	// --ao=null decodes for real but never opens a sound device.
	m, err := StartMpv(home.Audio{Channels: "auto", Extra: []string{"--ao=null", "--media-controls=no"}}, 50)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

// wait collects events until one matches, or fails after a timeout.
func wait(t *testing.T, m *Mpv, seen *[]Event, match func(Event) bool) Event {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case e := <-m.Events():
			*seen = append(*seen, e)
			if e.Kind == Exited && !match(e) {
				t.Fatalf("mpv exited: %v", e.Err)
			}
			if match(e) {
				return e
			}
		case <-deadline:
			t.Fatalf("timed out; saw %+v", *seen)
		}
	}
}

func TestMpvPlaysGaplesslyAndReportsHiResParams(t *testing.T) {
	m := startTestMpv(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.wav"), filepath.Join(dir, "b.wav")
	writeWAV(t, a, 96000, 2, 600*time.Millisecond)
	writeWAV(t, b, 96000, 6, 600*time.Millisecond)

	var seen []Event
	if err := m.Play(a); err != nil {
		t.Fatal(err)
	}
	if err := m.Next(b); err != nil {
		t.Fatal(err)
	}
	wait(t, m, &seen, func(e Event) bool { return e.Kind == Started && e.Path == a })
	src := wait(t, m, &seen, func(e Event) bool { return e.Kind == Params && e.Source.SampleRate > 0 })
	if src.Source.SampleRate != 96000 || src.Source.Channels != 2 {
		t.Fatalf("source params %+v", src.Source)
	}
	end := wait(t, m, &seen, func(e Event) bool { return e.Kind == Ended })
	if end.Reason != EOF || !end.Continues {
		t.Fatalf("first track end %+v", end)
	}
	wait(t, m, &seen, func(e Event) bool { return e.Kind == Started && e.Path == b })
	six := wait(t, m, &seen, func(e Event) bool { return e.Kind == Params && e.Source.Channels == 6 })
	if six.Source.Layout == "" {
		t.Fatalf("surround layout missing: %+v", six.Source)
	}
	last := wait(t, m, &seen, func(e Event) bool { return e.Kind == Ended })
	if last.Reason != EOF || last.Continues {
		t.Fatalf("last track end %+v", last)
	}
}

func TestMpvControls(t *testing.T) {
	m := startTestMpv(t)
	a := filepath.Join(t.TempDir(), "a.wav")
	writeWAV(t, a, 44100, 2, 3*time.Second)
	var seen []Event
	m.Play(a)
	wait(t, m, &seen, func(e Event) bool { return e.Kind == Duration && e.Duration > 2*time.Second })
	m.SetPaused(true)
	wait(t, m, &seen, func(e Event) bool { return e.Kind == Paused && e.Paused })
	m.Seek(2 * time.Second)
	// An exact seek while paused can settle a little early (mpv's audio
	// pre-roll), so accept anything clearly past the start.
	wait(t, m, &seen, func(e Event) bool { return e.Kind == Position && e.Pos >= 1500*time.Millisecond })
	if err := m.SetVolume(250); err != nil { // clamped to 100, never amplified
		t.Fatal(err)
	}
	m.Stop()
	e := wait(t, m, &seen, func(e Event) bool { return e.Kind == Ended })
	if e.Reason != Stopped {
		t.Fatalf("stop reason %v", e.Reason)
	}
}

func TestMpvBadFileReportsError(t *testing.T) {
	m := startTestMpv(t)
	bad := filepath.Join(t.TempDir(), "bad.flac")
	os.WriteFile(bad, []byte("not audio"), 0o644)
	var seen []Event
	m.Play(bad)
	e := wait(t, m, &seen, func(e Event) bool { return e.Kind == Ended })
	if e.Reason != Error || e.Err == nil {
		t.Fatalf("%+v", e)
	}
}

func TestArgsAreSafeByDefault(t *testing.T) {
	args := Args(home.Default().Audio, 300)
	for _, want := range []string{"--no-config", "--volume-max=100", "--volume=100", "--audio-channels=auto-safe"} {
		if !slices.Contains(args, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	for _, a := range args {
		if a == "--audio-exclusive=yes" || len(a) > 13 && a[:13] == "--audio-spdif" {
			t.Errorf("unsafe default %s", a)
		}
	}
}

func TestMediaKeysReachKyro(t *testing.T) {
	m := startTestMpv(t)
	var seen []Event
	for key, want := range map[string]string{
		"NEXT": RemoteNext, "PREV": RemotePrev, "STOP": RemoteStop,
		// The keyboard's play/pause key arrives as PLAY and must toggle.
		"PLAY": RemoteToggle, "PLAYPAUSE": RemoteToggle, "PAUSE": RemoteToggle,
		"PLAYONLY": RemotePlay, "PAUSEONLY": RemotePause,
	} {
		// keypress is what mpv does when the system sends it a media key.
		if err := m.command("keypress", key); err != nil {
			t.Fatal(err)
		}
		e := wait(t, m, &seen, func(e Event) bool { return e.Kind == Remote })
		if e.Command != want {
			t.Errorf("%s: got %q, want %q", key, e.Command, want)
		}
	}
}

func TestMediaControlsFollowConfig(t *testing.T) {
	if !slices.Contains(Args(home.Audio{}, 50), "--media-controls=yes") {
		t.Error("media keys are on by default")
	}
	if !slices.Contains(Args(home.Audio{MediaKeys: "off"}, 50), "--media-controls=no") {
		t.Error(`"mediaKeys": "off" should turn them off`)
	}
}

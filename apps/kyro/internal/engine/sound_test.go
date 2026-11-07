package engine

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/home"
	"github.com/karim-w/slop/apps/kyro/internal/sound"
)

// writeLoud writes 3 s of a near full-scale stereo mix of 100 Hz (bass)
// and 8 kHz (air), the worst case for modes that boost both.
func writeLoud(t *testing.T, path string) {
	writeStereo(t, path, func(x float64) (float64, float64) {
		return 0.7*math.Sin(2*math.Pi*100*x) + 0.29*math.Sin(2*math.Pi*8000*x),
			0.7*math.Sin(2*math.Pi*100*x) - 0.29*math.Sin(2*math.Pi*8000*x)
	})
}

// writeStereo writes 3 s of 16-bit stereo at 48 kHz from f(seconds).
func writeStereo(t *testing.T, path string, f func(x float64) (l, r float64)) {
	t.Helper()
	const rate = 48000
	var data []byte
	for i := 0; i < rate*3; i++ {
		l, r := f(float64(i) / rate)
		for _, v := range []float64{l, r} {
			data = binary.LittleEndian.AppendUint16(data, uint16(int16(v*32767)))
		}
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(data)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], 2)
	binary.LittleEndian.PutUint32(h[24:], rate)
	binary.LittleEndian.PutUint32(h[28:], rate*4)
	binary.LittleEndian.PutUint16(h[32:], 4)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(data)))
	if err := os.WriteFile(path, append(h, data...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// render runs mpv with the mode's filters into a raw float file (never a
// sound device) and returns the samples.
func render(t *testing.T, in, mode string) []float32 {
	t.Helper()
	bin, err := MpvPath("")
	if err != nil {
		t.Skip("mpv not installed")
	}
	out := filepath.Join(t.TempDir(), "out.raw")
	args := []string{"--no-config", "--no-video", "--really-quiet", "--ao=pcm", "--ao-pcm-file=" + out,
		"--ao-pcm-waveheader=no", "--audio-format=float", "--audio-channels=stereo"}
	if af := sound.Filter(mode); af != "" {
		args = append(args, "--af="+af)
	}
	if b, err := exec.Command(bin, append(args, in)...).CombinedOutput(); err != nil {
		t.Fatalf("mpv %s: %v\n%s", mode, err, b)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := make([]float32, len(raw)/4)
	for i := range s {
		s[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return s
}

func peak(s []float32) float64 {
	p := 0.0
	for _, v := range s {
		p = max(p, math.Abs(float64(v)))
	}
	return p
}

// width is the RMS of the side (L-R) over the RMS of the mid (L+R): how
// wide the stereo is, whatever the overall level.
func width(s []float32) float64 {
	var side, mid float64
	for i := 0; i+1 < len(s); i += 2 {
		d, m := float64(s[i]-s[i+1]), float64(s[i]+s[i+1])
		side += d * d
		mid += m * m
	}
	return math.Sqrt(side / mid)
}

func TestSoundModesStayUnderFullScale(t *testing.T) {
	in := filepath.Join(t.TempDir(), "loud.wav")
	writeLoud(t, in)
	pure := render(t, in, "pure")
	if p := peak(pure); p < 0.95 {
		t.Fatalf("pure should be untouched, peak %.3f", p)
	}
	for _, m := range sound.Modes[1:] {
		t.Run(m.Name, func(t *testing.T) {
			s := render(t, in, m.Name)
			// The limiter holds -1 dBFS (0.891); allow for its look-ahead.
			if p := peak(s); p > 0.9 {
				t.Errorf("peak %.3f (%.1f dBFS), want ≤ -1 dBFS", p, 20*math.Log10(p))
			}
			if p := peak(s); p < 0.5 {
				t.Errorf("peak %.3f: the mode shouldn't just turn things down", p)
			}
		})
	}
	// One tone, louder on the left: only the stereo image can change.
	lr := filepath.Join(t.TempDir(), "lr.wav")
	writeStereo(t, lr, func(x float64) (float64, float64) {
		v := math.Sin(2 * math.Pi * 1000 * x)
		return 0.4 * v, 0.2 * v
	})
	if w, p := width(render(t, lr, "wide")), width(render(t, lr, "pure")); w < p*1.2 {
		t.Errorf("wide should widen the stereo: %.3f vs pure %.3f", w, p)
	}
}

func TestMpvArgsCarryTheSoundMode(t *testing.T) {
	if args := Args(home.Audio{}, 50); slices.ContainsFunc(args, func(a string) bool { return len(a) > 5 && a[:5] == "--af=" }) {
		t.Errorf("pure adds no filters: %v", args)
	}
	if args := Args(home.Audio{Sound: "wide"}, 50); !slices.Contains(args, "--af="+sound.Filter("wide")) {
		t.Errorf("wide should be passed to mpv: %v", args)
	}
	if !slices.Contains(Args(home.Audio{}, 100), "--volume-max=100") {
		t.Error("the volume cap must stay")
	}
}

func TestMpvSwitchesSoundWhilePlaying(t *testing.T) {
	m := startTestMpv(t)
	path := filepath.Join(t.TempDir(), "a.wav")
	writeWAV(t, path, 44100, 2, 2*time.Second)
	var seen []Event
	if err := m.Play(path); err != nil {
		t.Fatal(err)
	}
	wait(t, m, &seen, func(e Event) bool { return e.Kind == Started })
	for _, mode := range []string{"wide", "deep", "pure", "punch"} {
		if err := m.SetFilter(sound.Filter(mode)); err != nil {
			t.Fatal(err)
		}
	}
	// It keeps playing to the end.
	wait(t, m, &seen, func(e Event) bool { return e.Kind == Ended && e.Reason == EOF })
}

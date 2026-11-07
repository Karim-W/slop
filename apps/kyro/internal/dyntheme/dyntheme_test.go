package dyntheme

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/karim-w/slop/pkg/domain/theme"
)

// cover paints a background with a disc of another colour in the middle.
func cover(bg, disc color.RGBA, radius float64) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 300; x++ {
			c := bg
			if math.Hypot(float64(x-150), float64(y-150)) < radius {
				c = disc
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func hue(hex string) float64 { return fromColor(theme.Hex(hex)).h() * 180 / math.Pi }

func hueNear(t *testing.T, what, hex string, want float64) {
	t.Helper()
	d := math.Abs(math.Remainder(hue(hex)-want, 360))
	if d > 25 {
		t.Errorf("%s %s has hue %.0f°, want about %.0f°", what, hex, hue(hex), want)
	}
}

// readableOn checks every text-like colour of th against its background.
func readableOn(t *testing.T, th theme.Theme) {
	t.Helper()
	bg := theme.Hex(th.Bg)
	for name, c := range map[string]string{
		"text": th.Text, "subtext": th.Subtext, "accent": th.Accent, "accent2": th.Accent2,
		"accent3": th.Accent3, "success": th.Success, "warning": th.Warning, "error": th.Error, "info": th.Info,
	} {
		if r := theme.Contrast(theme.Hex(c), bg); r < 4.4 {
			t.Errorf("%s %s on %s: contrast %.2f, want ≥ 4.5", name, c, th.Bg, r)
		}
	}
	if r := theme.Contrast(theme.Hex(th.Muted), bg); r < 2.9 {
		t.Errorf("muted %s on %s: contrast %.2f, want ≥ 3", th.Muted, th.Bg, r)
	}
}

func TestDarkCoverMakesADarkThemeInItsColours(t *testing.T) {
	navy, orange := color.RGBA{18, 32, 92, 255}, color.RGBA{250, 140, 30, 255}
	th := From(cover(navy, orange, 70), theme.Default())
	if !th.Dark || th.Name != Name {
		t.Fatalf("got %q dark=%v, want a dark %q theme", th.Name, th.Dark, Name)
	}
	if theme.Luminance(theme.Hex(th.Bg)) > 0.06 {
		t.Errorf("background %s is not dark", th.Bg)
	}
	hueNear(t, "background", th.Bg, hue("#12205c"))
	hueNear(t, "accent", th.Accent, hue("#fa8c1e"))
	readableOn(t, th)
	if th.Accent == th.Accent2 || th.Accent2 == th.Accent3 {
		t.Errorf("accents should differ for gradients: %s %s %s", th.Accent, th.Accent2, th.Accent3)
	}
	if len(th.Graph) < 3 {
		t.Errorf("graph lanes: %v", th.Graph)
	}
}

func TestLightCoverMakesALightTheme(t *testing.T) {
	paper, red := color.RGBA{245, 240, 230, 255}, color.RGBA{200, 20, 40, 255}
	th := From(cover(paper, red, 50), theme.Default())
	if th.Dark {
		t.Fatalf("a mostly white cover should give a light theme, got bg %s", th.Bg)
	}
	hueNear(t, "accent", th.Accent, hue("#c81428"))
	readableOn(t, th)
}

func TestGreyCoverStillReadable(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetGray(x, y, color.Gray{uint8(x * 2)})
		}
	}
	th := From(img, theme.Default())
	readableOn(t, th)
	if th.Accent == th.Text {
		t.Errorf("accent should still stand apart from text")
	}
}

func TestStatusColoursKeepTheirMeaning(t *testing.T) {
	base := theme.Default()
	th := From(cover(color.RGBA{10, 60, 40, 255}, color.RGBA{120, 220, 160, 255}, 60), base)
	hueNear(t, "error", th.Error, hue(base.Error))
	hueNear(t, "success", th.Success, hue(base.Success))
}

func TestBlend(t *testing.T) {
	a, b := theme.Default(), From(cover(color.RGBA{0, 0, 0, 255}, color.RGBA{0, 200, 255, 255}, 80), theme.Default())
	if got := Blend(a, b, 0); got.Bg != a.Bg {
		t.Errorf("t=0 bg %s, want %s", got.Bg, a.Bg)
	}
	if got := Blend(a, b, 1); got.Bg != b.Bg || got.Name != Name {
		t.Errorf("t=1 bg %s %q, want %s", got.Bg, got.Name, b.Bg)
	}
	mid := Blend(a, b, 0.5)
	if mid.Bg == a.Bg || mid.Bg == b.Bg || len(mid.Graph) != len(b.Graph) {
		t.Errorf("t=0.5 should sit between: %s (from %s to %s)", mid.Bg, a.Bg, b.Bg)
	}
}

func TestEmptyImageFallsBack(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8)) // fully transparent
	if got := From(img, theme.Default()); got.Name != theme.Default().Name {
		t.Errorf("transparent image should keep the base theme, got %q", got.Name)
	}
}

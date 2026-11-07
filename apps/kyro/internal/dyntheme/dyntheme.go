// Package dyntheme derives a whole colour theme from a cover image, so the
// player can take on the colours of the album that is playing: background,
// text, accents, gradients, borders and selection all come from the art.
//
// Colours are picked and adjusted in OKLab, a perceptual space, so lightness
// and saturation behave the way the eye expects. Every text colour is
// checked for contrast against the derived background, whatever the cover.
package dyntheme

import (
	"image"
	"image/color"
	"math"
	"sort"

	"golang.org/x/image/draw"

	"github.com/karim-w/slop/pkg/domain/theme"
)

// Name is the name of every derived theme.
const Name = "cover"

// From returns the theme of img. base supplies the hues of the status
// colours (success, warning, error, info), which keep their meaning.
func From(img image.Image, base theme.Theme) theme.Theme {
	sw, avgL := swatches(img)
	if len(sw) == 0 {
		return base
	}
	dom := sw[0]
	dark := avgL < 0.72

	// The background carries the cover's main hue, quietly. A grey cover
	// borrows the hue of its most colourful part, if it has one.
	hue, chroma := dom.h(), dom.c()
	if chroma < 0.02 {
		for _, s := range sw {
			if s.c() >= 0.05 && s.w >= 0.05 {
				hue, chroma = s.h(), 0.03
				break
			}
		}
	}
	var bg lab
	if dark {
		bg = lch(clamp(dom.L*0.32, 0.15, 0.24), math.Min(chroma*0.5, 0.045), hue)
	} else {
		bg = lch(0.965, math.Min(chroma*0.3, 0.025), hue)
	}
	bgc := bg.color()

	tone := func(l, c float64) lab { return lch(l, c, hue) }
	var text, sub, muted lab
	if dark {
		text, sub, muted = tone(0.95, 0.012), tone(0.81, 0.03), tone(0.62, 0.035)
	} else {
		text, sub, muted = tone(0.24, 0.02), tone(0.40, 0.035), tone(0.56, 0.035)
	}
	muted = readable(muted, bgc, 3, dark)

	// Accents: the most vivid colours of the cover, weighted a little by how
	// much of it they cover, then made readable on the background.
	accents := pickAccents(sw, 3, hue, chroma >= 0.02)
	var acc [3]lab
	for i := range acc {
		switch {
		case i < len(accents):
			acc[i] = accents[i]
		case len(accents) > 0:
			// Not enough distinct colours: rotate the main accent's hue.
			a := accents[0]
			acc[i] = lch(a.L, a.c(), a.h()+[]float64{0, 0.6, -0.6}[i])
		default:
			// A monochrome cover: a soft tint of its own hue.
			acc[i] = lch(0.84-0.06*float64(i), math.Max(chroma, 0.03), hue+0.3*float64(i))
		}
		acc[i] = accent(acc[i], bgc, dark)
	}

	h := func(c lab) string { return theme.ToHex(c.color()) }
	status := func(hex string) string { return h(accent(fromColor(theme.Hex(hex)), bgc, dark)) }
	mix := func(a, b lab, t float64) string { return theme.ToHex(theme.Mix(a.color(), b.color(), t)) }

	t := theme.Theme{
		Name:      Name,
		Dark:      dark,
		Bg:        h(bg),
		Surface:   mix(bg, text, 0.06),
		Overlay:   mix(bg, text, 0.10),
		Border:    mix(bg, acc[0], 0.28),
		Text:      h(text),
		Subtext:   h(sub),
		Muted:     h(muted),
		Accent:    h(acc[0]),
		Accent2:   h(acc[1]),
		Accent3:   h(acc[2]),
		Success:   status(base.Success),
		Warning:   status(base.Warning),
		Error:     status(base.Error),
		Info:      status(base.Info),
		Added:     status(base.Added),
		Removed:   status(base.Removed),
		Modified:  status(base.Modified),
		Selection: mix(bg, acc[0], 0.24),
	}
	for _, a := range acc {
		t.Graph = append(t.Graph, h(a))
	}
	for _, s := range sw {
		if len(t.Graph) >= 8 {
			break
		}
		if s.c() >= 0.04 && farFrom(s, acc[:], 0.5) {
			t.Graph = append(t.Graph, h(accent(s.lab, bgc, dark)))
		}
	}
	t.Graph = append(t.Graph, t.Info, t.Success, t.Warning)
	return t
}

// Blend mixes two themes, for a smooth change from one cover to the next.
func Blend(a, b theme.Theme, t float64) theme.Theme {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	m := func(x, y string) string {
		if x == "" {
			return y
		}
		if y == "" {
			return x
		}
		return theme.ToHex(theme.Mix(theme.Hex(x), theme.Hex(y), t))
	}
	o := b
	o.Bg, o.Surface, o.Overlay, o.Border = m(a.Bg, b.Bg), m(a.Surface, b.Surface), m(a.Overlay, b.Overlay), m(a.Border, b.Border)
	o.Text, o.Subtext, o.Muted = m(a.Text, b.Text), m(a.Subtext, b.Subtext), m(a.Muted, b.Muted)
	o.Accent, o.Accent2, o.Accent3 = m(a.Accent, b.Accent), m(a.Accent2, b.Accent2), m(a.Accent3, b.Accent3)
	o.Success, o.Warning, o.Error, o.Info = m(a.Success, b.Success), m(a.Warning, b.Warning), m(a.Error, b.Error), m(a.Info, b.Info)
	o.Added, o.Removed, o.Modified = m(a.Added, b.Added), m(a.Removed, b.Removed), m(a.Modified, b.Modified)
	o.Selection = m(a.Selection, b.Selection)
	o.Graph = make([]string, len(b.Graph))
	for i, g := range b.Graph {
		from := g
		if len(a.Graph) > 0 {
			from = a.Graph[i%len(a.Graph)]
		}
		o.Graph[i] = m(from, g)
	}
	return o
}

// ─── Swatches ───────────────────────────────────────────────────────────────

// swatch is one colour of the cover and the share of the cover it takes.
type swatch struct {
	lab
	w float64
}

// swatches returns the cover's main colours, largest first, and its mean
// lightness. The image is shrunk to 48×48, its pixels binned in OKLab, and
// neighbouring bins merged.
func swatches(img image.Image) ([]swatch, float64) {
	const n = 48
	small := image.NewRGBA(image.Rect(0, 0, n, n))
	draw.ApproxBiLinear.Scale(small, small.Bounds(), img, img.Bounds(), draw.Src, nil)

	type bin struct {
		sum lab
		n   float64
	}
	bins := map[[3]int]*bin{}
	var total, sumL float64
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			c := small.RGBAAt(x, y)
			if c.A < 128 {
				continue
			}
			p := fromColor(c)
			k := [3]int{int(p.L * 10), int((p.A + 0.4) * 18), int((p.B + 0.4) * 18)}
			b := bins[k]
			if b == nil {
				b = &bin{}
				bins[k] = b
			}
			b.sum.L += p.L
			b.sum.A += p.A
			b.sum.B += p.B
			b.n++
			total++
			sumL += p.L
		}
	}
	if total == 0 {
		return nil, 0
	}
	raw := make([]swatch, 0, len(bins))
	for _, b := range bins {
		raw = append(raw, swatch{lab{b.sum.L / b.n, b.sum.A / b.n, b.sum.B / b.n}, b.n / total})
	}
	sort.Slice(raw, func(i, j int) bool {
		if raw[i].w != raw[j].w {
			return raw[i].w > raw[j].w
		}
		return raw[i].L < raw[j].L // stable order for equal bins
	})
	var out []swatch
	for _, s := range raw {
		merged := false
		for i := range out {
			if dist(out[i].lab, s.lab) < 0.09 {
				w := out[i].w + s.w
				out[i].lab = lab{
					(out[i].L*out[i].w + s.L*s.w) / w,
					(out[i].A*out[i].w + s.A*s.w) / w,
					(out[i].B*out[i].w + s.B*s.w) / w,
				}
				out[i].w = w
				merged = true
				break
			}
		}
		if !merged && len(out) < 16 {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].w > out[j].w })
	return out, sumL / total
}

// pickAccents returns up to n vivid colours with clearly different hues.
// Colours that stand out from the background's hue score higher, so the
// accent pops instead of blending in; a cover in one hue still uses it.
func pickAccents(sw []swatch, n int, bgHue float64, tinted bool) []lab {
	type cand struct {
		c     lab
		score float64
	}
	var cs []cand
	for _, s := range sw {
		if s.c() < 0.045 || s.w < 0.004 {
			continue
		}
		score := s.c() * math.Pow(s.w, 0.3)
		if tinted && !farFrom(s, []lab{lch(0.5, 0.1, bgHue)}, 0.6) {
			score *= 0.5
		}
		cs = append(cs, cand{s.lab, score})
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].score > cs[j].score })
	var out []lab
	for _, c := range cs {
		if len(out) == n {
			break
		}
		if farFrom(swatch{lab: c.c}, out, 0.5) {
			out = append(out, c.c)
		}
	}
	return out
}

// farFrom reports whether s's hue is at least minHue radians from every
// colour in others.
func farFrom(s swatch, others []lab, minHue float64) bool {
	for _, o := range others {
		d := math.Abs(math.Remainder(s.h()-o.h(), 2*math.Pi))
		if d < minHue {
			return false
		}
	}
	return true
}

// accent makes c a vivid colour readable on bg: lightness in a band that
// suits the background, then lifted (or deepened) until it has 4.5:1
// contrast, keeping its hue.
func accent(c lab, bg color.Color, dark bool) lab {
	l, ch := c.L, math.Min(c.c()*1.1, 0.2)
	if dark {
		l = clamp(l, 0.72, 0.88)
	} else {
		l = clamp(l, 0.40, 0.55)
	}
	return readable(lch(l, ch, c.h()), bg, 4.5, dark)
}

// readable moves c's lightness away from bg until the contrast reaches want.
func readable(c lab, bg color.Color, want float64, dark bool) lab {
	step := 0.02
	if !dark {
		step = -step
	}
	for i := 0; i < 40 && theme.Contrast(c.color(), bg) < want; i++ {
		c = lch(clamp(c.L+step, 0, 1), c.c(), c.h())
	}
	return c
}

// ─── OKLab ──────────────────────────────────────────────────────────────────

type lab struct{ L, A, B float64 }

func (c lab) c() float64 { return math.Hypot(c.A, c.B) }
func (c lab) h() float64 { return math.Atan2(c.B, c.A) }

func lch(l, c, h float64) lab { return lab{l, c * math.Cos(h), c * math.Sin(h)} }

func dist(a, b lab) float64 {
	return math.Sqrt(2*(a.L-b.L)*(a.L-b.L) + (a.A-b.A)*(a.A-b.A) + (a.B-b.B)*(a.B-b.B))
}

func fromColor(c color.Color) lab {
	r8, g8, b8, _ := c.RGBA()
	r, g, b := toLinear(float64(r8>>8)/255), toLinear(float64(g8>>8)/255), toLinear(float64(b8>>8)/255)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return lab{
		0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

func (c lab) linear() (r, g, b float64) {
	l := c.L + 0.3963377774*c.A + 0.2158037573*c.B
	m := c.L - 0.1055613458*c.A - 0.0638541728*c.B
	s := c.L - 0.0894841775*c.A - 1.2914855480*c.B
	l, m, s = l*l*l, m*m*m, s*s*s
	return 4.0767416621*l - 3.3077115913*m + 0.2309699292*s,
		-1.2684380046*l + 2.6097574011*m - 0.3413193965*s,
		-0.0041960863*l - 0.7034186147*m + 1.7076147010*s
}

// color converts to sRGB, lowering chroma until the colour fits the gamut.
func (c lab) color() color.Color {
	ch, h := c.c(), c.h()
	for i := 0; i < 24; i++ {
		r, g, b := lch(c.L, ch, h).linear()
		if inGamut(r) && inGamut(g) && inGamut(b) {
			break
		}
		ch *= 0.85
	}
	r, g, b := lch(c.L, ch, h).linear()
	to8 := func(v float64) uint8 { return uint8(math.Round(clamp(fromLinear(v), 0, 1) * 255)) }
	return color.RGBA{to8(r), to8(g), to8(b), 255}
}

func inGamut(v float64) bool { return v >= -1e-4 && v <= 1+1e-4 }

func toLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func fromLinear(v float64) float64 {
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

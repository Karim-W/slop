// Package art finds cover art and draws it in the terminal.
//
// Drawing uses Unicode block elements in 24-bit colour. In the default
// "quadrant" mode every cell holds a 2×2 grid of pixels: for each cell kyro
// tries all eight ways to split the four pixels into two groups and keeps
// the split whose two average colours match the pixels best, then draws it
// as one quadrant glyph (▘▝▀▖▌▞▛ …) with those colours. That doubles the
// horizontal detail of the common half-block (▀) technique, which is kept as
// the "halfblock" mode for fonts that lack quadrant glyphs.
//
// Scaling averages pixels in linear light rather than sRGB, so heavy
// downscaling (a 1000-pixel cover into 40 cells) keeps the cover's real
// brightness and colours instead of going muddy and dark.
package art

import (
	"image"
	"math"
	"strconv"
	"strings"
)

// Mode is how pixels map to cells.
type Mode string

const (
	// Auto draws real pixels where the terminal supports the kitty graphics
	// protocol, and quadrant blocks elsewhere.
	Auto Mode = "auto"
	// PixelsMode always uses the kitty graphics protocol.
	PixelsMode Mode = "pixels"
	// Sextant uses 2×3 sub-pixels per cell (Unicode 13 "Symbols for Legacy
	// Computing"): the most detailed text mode, if your font has the glyphs.
	Sextant   Mode = "sextant"
	Quadrant  Mode = "quadrant"
	HalfBlock Mode = "halfblock"
	// ASCII draws with characters from a density ramp, each in the colour
	// of the image under it.
	ASCII Mode = "ascii"
	// ASCIIMono is ASCII in the terminal's text colour, the classic look.
	ASCIIMono Mode = "ascii-mono"
)

// Modes lists every style, in the order the picker shows them.
var Modes = []Mode{Auto, Sextant, Quadrant, HalfBlock, ASCII, ASCIIMono}

// Describe says what a style looks like.
func Describe(m Mode) string {
	switch m {
	case Auto, PixelsMode:
		return "the real image where the terminal can show pictures"
	case Sextant:
		return "2×3 colour blocks per cell, needs a recent font"
	case Quadrant:
		return "2×2 colour blocks per cell"
	case HalfBlock:
		return "1×2 colour blocks per cell, works anywhere"
	case ASCII:
		return "coloured characters, like classic ASCII art"
	case ASCIIMono:
		return "plain characters in your text colour"
	}
	return ""
}

// Render draws img into exactly w×h cells, fitting it inside and centring
// it; the margins are left as plain spaces.
func Render(img image.Image, w, h int, mode Mode) []string {
	out := make([]string, max(h, 0))
	if img == nil || w <= 0 || h <= 0 {
		for i := range out {
			out[i] = strings.Repeat(" ", max(w, 0))
		}
		return out
	}
	// Cells are about twice as tall as wide. Fit the image's aspect into
	// w × 2h square units.
	b := img.Bounds()
	aspect := float64(b.Dx()) / float64(b.Dy())
	cw, ch := w, int(math.Round(float64(w)/aspect/2))
	if ch > h {
		ch = h
		cw = min(w, int(math.Round(float64(h)*2*aspect)))
	}
	cw, ch = max(cw, 1), max(ch, 1)
	left, top := (w-cw)/2, (h-ch)/2

	var cells [][]string
	switch mode {
	case HalfBlock:
		cells = halfBlock(sample(img, cw, ch*2), cw, ch)
	case Sextant:
		cells = sextant(sample(img, cw*2, ch*3), cw, ch)
	case ASCII, ASCIIMono:
		cells = asciiArt(sample(img, cw, ch), cw, ch, mode == ASCII)
	default:
		cells = quadrant(sample(img, cw*2, ch*2), cw, ch)
	}
	pad := func(n int) string { return strings.Repeat(" ", max(n, 0)) }
	for y := 0; y < h; y++ {
		if y < top || y >= top+ch {
			out[y] = pad(w)
			continue
		}
		out[y] = pad(left) + strings.Join(cells[y-top], "") + "\x1b[0m" + pad(w-left-cw)
	}
	return out
}

// rgb is a colour in linear light, 0–1 per channel.
type rgb struct{ r, g, b float64 }

func (a rgb) add(b rgb) rgb       { return rgb{a.r + b.r, a.g + b.g, a.b + b.b} }
func (a rgb) scale(f float64) rgb { return rgb{a.r * f, a.g * f, a.b * f} }
func (a rgb) dist(b rgb) float64 {
	// weight green highest, as the eye does
	dr, dg, db := a.r-b.r, a.g-b.g, a.b-b.b
	return 0.30*dr*dr + 0.59*dg*dg + 0.11*db*db
}

var toLinear = func() (t [256]float64) {
	for i := range t {
		c := float64(i) / 255
		if c <= 0.04045 {
			t[i] = c / 12.92
		} else {
			t[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return
}()

func toSRGB(c float64) int {
	if c <= 0.0031308 {
		c *= 12.92
	} else {
		c = 1.055*math.Pow(c, 1/2.4) - 0.055
	}
	return min(max(int(math.Round(c*255)), 0), 255)
}

// sample area-averages img onto a pw×ph grid in linear light.
func sample(img image.Image, pw, ph int) [][]rgb {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	grid := make([][]rgb, ph)
	// Precompute each source row's and column's target cell.
	for y := range grid {
		grid[y] = make([]rgb, pw)
	}
	counts := make([][]float64, ph)
	for y := range counts {
		counts[y] = make([]float64, pw)
	}
	// Skip pixels on very large images: averaging every 2nd/3rd pixel of a
	// 3000-pixel cover is indistinguishable and much faster.
	step := max(1, min(sw/(pw*8), sh/(ph*8)))
	for sy := 0; sy < sh; sy += step {
		ty := sy * ph / sh
		for sx := 0; sx < sw; sx += step {
			tx := sx * pw / sw
			r, g, bb, a := img.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
			if a == 0 {
				continue
			}
			// un-premultiply, then linearise
			af := float64(a) / 0xffff
			c := rgb{
				toLinear[min(int(float64(r>>8)/af), 255)],
				toLinear[min(int(float64(g>>8)/af), 255)],
				toLinear[min(int(float64(bb>>8)/af), 255)],
			}
			grid[ty][tx] = grid[ty][tx].add(c.scale(af))
			counts[ty][tx] += af
		}
	}
	for y := range grid {
		for x := range grid[y] {
			if n := counts[y][x]; n > 0 {
				grid[y][x] = grid[y][x].scale(1 / n)
			}
		}
	}
	return grid
}

func sgr(fg, bg rgb) string {
	var b strings.Builder
	b.Grow(40)
	b.WriteString("\x1b[38;2;")
	writeRGB(&b, fg)
	b.WriteString(";48;2;")
	writeRGB(&b, bg)
	b.WriteByte('m')
	return b.String()
}

func writeRGB(b *strings.Builder, c rgb) {
	b.WriteString(strconv.Itoa(toSRGB(c.r)))
	b.WriteByte(';')
	b.WriteString(strconv.Itoa(toSRGB(c.g)))
	b.WriteByte(';')
	b.WriteString(strconv.Itoa(toSRGB(c.b)))
}

func halfBlock(px [][]rgb, w, h int) [][]string {
	out := make([][]string, h)
	for y := 0; y < h; y++ {
		out[y] = make([]string, w)
		for x := 0; x < w; x++ {
			out[y][x] = sgr(px[2*y][x], px[2*y+1][x]) + "▀"
		}
	}
	return out
}

// glyphs maps a 4-bit mask (TL=1, TR=2, BL=4, BR=8; set = foreground) to
// its quadrant character.
var glyphs = [16]string{" ", "▘", "▝", "▀", "▖", "▌", "▞", "▛", "▗", "▚", "▐", "▜", "▄", "▙", "▟", "█"}

func quadrant(px [][]rgb, w, h int) [][]string {
	out := make([][]string, h)
	for y := 0; y < h; y++ {
		out[y] = make([]string, w)
		for x := 0; x < w; x++ {
			q := [4]rgb{px[2*y][2*x], px[2*y][2*x+1], px[2*y+1][2*x], px[2*y+1][2*x+1]}
			best, bestErr := 0, math.Inf(1)
			var bestFg, bestBg rgb
			// A mask and its complement are the same split, so masks 0–7
			// cover all eight partitions (0 = one flat colour).
			for mask := 0; mask < 8; mask++ {
				var fg, bg rgb
				nf, nb := 0.0, 0.0
				for i := 0; i < 4; i++ {
					if mask&(1<<i) != 0 {
						fg, nf = fg.add(q[i]), nf+1
					} else {
						bg, nb = bg.add(q[i]), nb+1
					}
				}
				if nf > 0 {
					fg = fg.scale(1 / nf)
				}
				if nb > 0 {
					bg = bg.scale(1 / nb)
				}
				e := 0.0
				for i := 0; i < 4; i++ {
					if mask&(1<<i) != 0 {
						e += q[i].dist(fg)
					} else {
						e += q[i].dist(bg)
					}
				}
				if e < bestErr {
					best, bestErr, bestFg, bestBg = mask, e, fg, bg
				}
			}
			if best == 0 {
				bestFg = bestBg
			}
			out[y][x] = sgr(bestFg, bestBg) + glyphs[best]
		}
	}
	return out
}

// sextantGlyph maps a 6-bit mask (bit 0 = top left, 1 = top right, 2 =
// middle left, … 5 = bottom right; set = foreground) to its character. The
// Unicode block U+1FB00 lists the patterns in mask order, skipping the four
// that already exist elsewhere: empty, left half, right half and full.
func sextantGlyph(mask int) string {
	switch mask {
	case 0:
		return " "
	case 21:
		return "▌"
	case 42:
		return "▐"
	case 63:
		return "█"
	}
	i := mask - 1
	if mask > 21 {
		i--
	}
	if mask > 42 {
		i--
	}
	return string(rune(0x1FB00 + i))
}

// sextant fits each cell's six pixels with the best of the 32 two-colour
// splits.
func sextant(px [][]rgb, w, h int) [][]string {
	out := make([][]string, h)
	var q [6]rgb
	for y := 0; y < h; y++ {
		out[y] = make([]string, w)
		for x := 0; x < w; x++ {
			for r := 0; r < 3; r++ {
				q[2*r], q[2*r+1] = px[3*y+r][2*x], px[3*y+r][2*x+1]
			}
			best, bestErr := 0, math.Inf(1)
			var bestFg, bestBg rgb
			// A mask and its complement are the same split: masks 0–31
			// cover all 32 of them.
			for mask := 0; mask < 32; mask++ {
				var fg, bg rgb
				nf, nb := 0.0, 0.0
				for i := 0; i < 6; i++ {
					if mask&(1<<i) != 0 {
						fg, nf = fg.add(q[i]), nf+1
					} else {
						bg, nb = bg.add(q[i]), nb+1
					}
				}
				if nf > 0 {
					fg = fg.scale(1 / nf)
				}
				if nb > 0 {
					bg = bg.scale(1 / nb)
				}
				e := 0.0
				for i := 0; i < 6; i++ {
					if mask&(1<<i) != 0 {
						e += q[i].dist(fg)
					} else {
						e += q[i].dist(bg)
					}
				}
				if e < bestErr {
					best, bestErr, bestFg, bestBg = mask, e, fg, bg
				}
			}
			if best == 0 {
				bestFg = bestBg
			}
			out[y][x] = sgr(bestFg, bestBg) + sextantGlyph(best)
		}
	}
	return out
}

// ramp runs from empty to dense: Paul Bourke's 70-level character ramp,
// which gives ASCII art its fine shading.
const ramp = " .'`^\",:;Il!i><~+_-?][}{1)(|\\/tfjrxnuvczXYUJCLQ0OZmwqpdbkhao*#MW&8%B@$"

// asciiArt picks a character per cell by brightness, coloured with the
// cell's own colour (or left in the text colour for the mono style). The
// contrast is stretched to the image's own range so dark covers still use
// the whole ramp.
func asciiArt(px [][]rgb, w, h int, colour bool) [][]string {
	lum := func(c rgb) float64 { // perceptual lightness of a linear colour
		return math.Pow(0.2126*c.r+0.7152*c.g+0.0722*c.b, 1/2.2)
	}
	lo, hi := 1.0, 0.0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			l := lum(px[y][x])
			lo, hi = math.Min(lo, l), math.Max(hi, l)
		}
	}
	span := math.Max(hi-lo, 0.05)
	runes := []rune(ramp)
	out := make([][]string, h)
	for y := 0; y < h; y++ {
		out[y] = make([]string, w)
		for x := 0; x < w; x++ {
			c := px[y][x]
			l := (lum(c) - lo) / span
			ch := string(runes[min(int(l*float64(len(runes)-1)+0.5), len(runes)-1)])
			if !colour {
				out[y][x] = ch
				continue
			}
			// Brighten the glyph colour a little: thin characters show less
			// colour than a solid block, so dim cells would vanish.
			lift := c.add(rgb{0.04, 0.04, 0.04}).scale(1.15)
			var b strings.Builder
			b.WriteString("\x1b[38;2;")
			writeRGB(&b, rgb{math.Min(lift.r, 1), math.Min(lift.g, 1), math.Min(lift.b, 1)})
			b.WriteString("m")
			b.WriteString(ch)
			out[y][x] = b.String()
		}
	}
	return out
}

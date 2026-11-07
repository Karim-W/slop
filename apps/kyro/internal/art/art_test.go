package art

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func solid(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestRenderFillsExactlyAndKeepsColour(t *testing.T) {
	for _, mode := range []Mode{Quadrant, HalfBlock} {
		lines := Render(solid(300, 300, color.RGBA{200, 40, 90, 255}), 20, 10, mode)
		if len(lines) != 10 {
			t.Fatalf("%s: %d lines", mode, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != 20 {
				t.Fatalf("%s line %d is %d cells", mode, i, w)
			}
		}
		if !strings.Contains(lines[5], "200;40;90") {
			t.Fatalf("%s lost the colour: %q", mode, lines[5])
		}
	}
}

func TestWideImagesAreLetterboxed(t *testing.T) {
	lines := Render(solid(400, 100, color.White), 20, 10, Quadrant)
	if strings.TrimSpace(ansi.Strip(lines[0])) != "" {
		t.Fatal("a 4:1 image in a square box should leave the top empty")
	}
}

func TestQuadrantPicksTheRightSplit(t *testing.T) {
	// left half black, right half white: one cell should be ▌ (left = fg)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.Black)
	img.Set(0, 1, color.Black)
	img.Set(1, 0, color.White)
	img.Set(1, 1, color.White)
	got := ansi.Strip(Render(img, 1, 1, Quadrant)[0])
	if got != "▌" && got != "▐" {
		t.Fatalf("got %q", got)
	}
}

func TestASCIIUsesTheRampAndColour(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			v := uint8(x * 6) // dark on the left, bright on the right
			img.Set(x, y, color.RGBA{v, v / 2, 255 - v, 255})
		}
	}
	for _, mode := range []Mode{ASCII, ASCIIMono} {
		lines := Render(img, 20, 10, mode)
		row := ansi.Strip(lines[5])
		if ansi.StringWidth(lines[5]) != 20 {
			t.Fatalf("%s width %d", mode, ansi.StringWidth(lines[5]))
		}
		if row[0] == row[len(row)-1] || !strings.ContainsAny(row, "@$B%8") {
			t.Fatalf("%s should run from sparse to dense: %q", mode, row)
		}
		if (mode == ASCII) != strings.Contains(lines[5], "38;2;") {
			t.Fatalf("%s colour mismatch", mode)
		}
	}
}

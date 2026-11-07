package art

import (
	"image"
	"image/color"
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPixelsDrawPlaceholdersAndQueueTheImage(t *testing.T) {
	p := NewPixels()
	p.SetCellSize(8, 16)
	lines := p.Cells("cover", noise(400, 400), 20, 10)
	if len(lines) != 10 {
		t.Fatalf("%d lines", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 20 {
			t.Fatalf("line %d is %d cells wide, want 20: placeholders must count as one cell", i, w)
		}
	}
	// A square image in 8×16 cells is twice as many columns as rows: 20×10.
	first := []rune(strings.TrimPrefix(lines[0], "\x1b[38;5;1m"))
	if first[0] != placeholder || first[1] != diacritics[0] || first[2] != diacritics[0] {
		t.Fatalf("row 0 col 0 marks wrong: %U", first[:3])
	}
	out := p.Flush()
	if !regexp.MustCompile(`^\x1b_Ga=T,U=1,f=100,i=1,c=20,r=10,q=2,m=1;`).MatchString(out) {
		t.Fatalf("transmission header: %q", out[:min(len(out), 80)])
	}
	if !strings.Contains(out, "\x1b_Gm=0,q=2;") {
		t.Fatal("chunked transfer must end with m=0")
	}
	// Same picture and size again: nothing new to send.
	p.Cells("cover", solid(400, 400, color.Black), 20, 10)
	if p.Pending() {
		t.Fatal("an image already sent was sent again")
	}
}

func TestPixelsLetterboxAndRecycleIDs(t *testing.T) {
	p := NewPixels()
	lines := p.Cells("wide", solid(800, 200, color.White), 20, 10) // 4:1
	blank := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			blank++
		}
	}
	if blank == 0 {
		t.Fatal("a wide image should leave blank rows above and below")
	}
	for i := 0; i < 300; i++ { // more images than ids
		p.Cells(string(rune('a'+i%26))+itoa(i), solid(4, 4, color.White), 2, 1)
	}
	if len(p.ids) > 255 || !strings.Contains(p.Flush(), "a=d,d=I") {
		t.Fatal("old images should be freed when ids run out")
	}
	cleanup := p.Cleanup()
	if strings.Count(cleanup, "a=d,d=I") != 255 {
		t.Fatalf("cleanup freed %d images", strings.Count(cleanup, "a=d,d=I"))
	}
	if l := p.Cells("after", solid(4, 4, color.White), 4, 2); strings.TrimSpace(l[0]) != "" || p.Pending() {
		t.Fatal("nothing may be drawn or sent after cleanup")
	}
}

func TestSextantGlyphs(t *testing.T) {
	for mask, want := range map[int]rune{1: 0x1FB00, 20: 0x1FB13, 22: 0x1FB14, 41: 0x1FB27, 43: 0x1FB28, 62: 0x1FB3B} {
		if got := []rune(sextantGlyph(mask))[0]; got != want {
			t.Errorf("mask %d: %U, want %U", mask, got, want)
		}
	}
	lines := Render(solid(90, 90, color.RGBA{1, 2, 3, 255}), 12, 6, Sextant)
	for _, l := range lines {
		if ansi.StringWidth(l) != 12 {
			t.Fatalf("width %d", ansi.StringWidth(l))
		}
	}
}

// noise is incompressible enough to need several 4 KiB chunks.
func noise(w, h int) image.Image {
	r := rand.New(rand.NewPCG(1, 2))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(r.IntN(256))
	}
	return img
}

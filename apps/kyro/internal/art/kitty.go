package art

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/draw"
)

// Pixels draws covers as real images with the kitty graphics protocol,
// which Rex, kitty, Ghostty and WezTerm speak.
//
// It uses the protocol's Unicode placeholders: the image is sent once to
// the terminal as a "virtual placement", and the UI then shows it by
// printing ordinary characters (U+10EEEE plus two marks giving the row and
// column, coloured with the image's id). To the UI those are just text
// cells, so layout, redraws, modals and scrolling all keep working, and the
// terminal paints the image's pixels in their place.
type Pixels struct {
	mu      sync.Mutex
	cellW   int // cell size in pixels, from the terminal (CSI 16 t)
	cellH   int
	ids     map[string]int // image key → id (1–255, carried in the 256-colour fg)
	lastUse map[int]uint64
	clock   uint64
	pending strings.Builder // escape sequences waiting to be written
	closed  bool            // after Cleanup: draw nothing, send nothing
}

// KittyQueryID is the image id KittyQuery uses; the answer carries it.
const KittyQueryID = 31

// KittyQuery asks whether the terminal supports the kitty graphics
// protocol; a capable terminal answers "Gi=31;OK".
const KittyQuery = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"

// CellSizeQuery asks for the cell size in pixels (answer: CSI 6;h;w t).
const CellSizeQuery = "\x1b[16t"

const placeholder = '\U0010EEEE'

// NewPixels returns a renderer assuming 8×16-pixel cells until told.
func NewPixels() *Pixels {
	return &Pixels{cellW: 8, cellH: 16, ids: map[string]int{}, lastUse: map[int]uint64{}}
}

// SetCellSize records the terminal's cell size in pixels.
func (p *Pixels) SetCellSize(w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if w != p.cellW || h != p.cellH {
		p.cellW, p.cellH = w, h
		p.dropAll() // sizes in pixels changed: re-send at the new resolution
	}
}

// Cells returns w×h cells showing img (fitted and centred) as placeholders,
// queueing the image for transmission if the terminal doesn't have it yet.
// key identifies the image (same key, same picture).
func (p *Pixels) Cells(key string, img image.Image, w, h int) []string {
	out := make([]string, h)
	p.mu.Lock()
	defer p.mu.Unlock()
	if img == nil || w <= 0 || h <= 0 || p.closed {
		for i := range out {
			out[i] = strings.Repeat(" ", max(w, 0))
		}
		return out
	}

	// Fit the image's real aspect into w×h cells using the true cell shape.
	b := img.Bounds()
	aspect := float64(b.Dx()) / float64(b.Dy())
	cw := w
	ch := int(math.Round(float64(w*p.cellW) / aspect / float64(p.cellH)))
	if ch > h {
		ch = h
		cw = min(w, int(math.Round(float64(h*p.cellH)*aspect/float64(p.cellW))))
	}
	cw, ch = max(min(cw, len(diacritics)), 1), max(min(ch, len(diacritics)), 1)

	id := p.ensure(fmt.Sprintf("%s|%dx%d", key, cw, ch), img, cw, ch)

	left, top := (w-cw)/2, (h-ch)/2
	color := fmt.Sprintf("\x1b[38;5;%dm", id)
	for y := 0; y < h; y++ {
		if y < top || y >= top+ch {
			out[y] = strings.Repeat(" ", w)
			continue
		}
		var s strings.Builder
		s.WriteString(strings.Repeat(" ", left))
		s.WriteString(color)
		row := diacritics[y-top]
		for x := 0; x < cw; x++ {
			s.WriteRune(placeholder)
			s.WriteRune(row)
			s.WriteRune(diacritics[x])
		}
		s.WriteString("\x1b[39m")
		s.WriteString(strings.Repeat(" ", w-left-cw))
		out[y] = s.String()
	}
	return out
}

// ensure returns the id of key's image, transmitting it if new.
func (p *Pixels) ensure(key string, img image.Image, cols, rows int) int {
	p.clock++
	if id, ok := p.ids[key]; ok {
		p.lastUse[id] = p.clock
		return id
	}
	id := p.freeID()
	p.ids[key] = id
	p.lastUse[id] = p.clock
	p.transmit(id, scaleTo(img, cols, rows, p.cellW, p.cellH), cols, rows)
	return id
}

// freeID picks an unused id, or recycles the least recently used one.
func (p *Pixels) freeID() int {
	for id := 1; id <= 255; id++ {
		if _, used := p.lastUse[id]; !used {
			return id
		}
	}
	oldest, at := 1, uint64(math.MaxUint64)
	for id, t := range p.lastUse {
		if t < at {
			oldest, at = id, t
		}
	}
	for k, id := range p.ids {
		if id == oldest {
			delete(p.ids, k)
		}
	}
	fmt.Fprintf(&p.pending, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", oldest)
	return oldest
}

// scaleTo resamples img to fill cols×rows cells at twice the reported cell
// resolution (crisp on HiDPI screens), capped to keep transfers small.
func scaleTo(img image.Image, cols, rows, cellW, cellH int) image.Image {
	pw, ph := cols*cellW*2, rows*cellH*2
	if m := max(pw, ph); m > 1200 {
		pw, ph = pw*1200/m, ph*1200/m
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(pw, 1), max(ph, 1)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	return dst
}

// transmit queues the image as PNG with a virtual placement of cols×rows,
// split into the protocol's 4096-byte chunks.
func (p *Pixels) transmit(id int, img image.Image, cols, rows int) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if enc.Encode(&buf, img) != nil {
		return
	}
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	const chunk = 4096
	for i := 0; i < len(data); i += chunk {
		end := min(i+chunk, len(data))
		more := 0
		if end < len(data) {
			more = 1
		}
		if i == 0 {
			fmt.Fprintf(&p.pending, "\x1b_Ga=T,U=1,f=100,i=%d,c=%d,r=%d,q=2,m=%d;%s\x1b\\", id, cols, rows, more, data[i:end])
		} else {
			fmt.Fprintf(&p.pending, "\x1b_Gm=%d,q=2;%s\x1b\\", more, data[i:end])
		}
	}
}

// Flush returns (and clears) the escape sequences to write to the terminal.
func (p *Pixels) Flush() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.pending.String()
	p.pending.Reset()
	return s
}

// Pending reports whether there is something to Flush.
func (p *Pixels) Pending() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pending.Len() > 0
}

// Cleanup returns the sequences that free every image kyro sent, for exit.
// The renderer draws and sends nothing afterwards.
func (p *Pixels) Cleanup() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.dropAll()
	s := p.pending.String()
	p.pending.Reset()
	return s
}

func (p *Pixels) dropAll() {
	for id := range p.lastUse {
		fmt.Fprintf(&p.pending, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
	}
	p.ids, p.lastUse = map[string]int{}, map[int]uint64{}
}

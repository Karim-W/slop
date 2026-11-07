package art

import (
	"bytes"
	"container/list"
	"crypto/sha1"
	"encoding/hex"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dhowden/tag"
	_ "golang.org/x/image/webp"

	"github.com/karim-w/slop/apps/kyro/internal/library"
)

// folderNames are the cover files kyro looks for next to a track, best first.
var folderNames = []string{"cover", "folder", "front", "album", "albumart", "albumartsmall", "artwork"}

var imageExts = []string{".jpg", ".jpeg", ".png", ".webp", ".gif"}

// Key identifies a track's artwork: tracks of one album share it.
func Key(t library.Track) string {
	h := sha1.Sum([]byte(filepath.Dir(t.Path) + "\x00" + strings.ToLower(t.Album) + "\x00" + boolStr(t.EmbeddedArt)))
	return hex.EncodeToString(h[:8])
}

func boolStr(b bool) string {
	if b {
		return "e"
	}
	return "f"
}

// Source returns the raw image bytes and their extension: the embedded
// picture first, then a cover file in the track's folder.
func Source(t library.Track) ([]byte, string, bool) {
	if t.EmbeddedArt {
		if f, err := os.Open(t.Path); err == nil {
			m, err := tag.ReadFrom(f)
			f.Close()
			if err == nil && m.Picture() != nil && len(m.Picture().Data) > 0 {
				ext := "." + strings.ToLower(m.Picture().Ext)
				if ext == "." {
					ext = ".jpg"
				}
				return m.Picture().Data, ext, true
			}
		}
	}
	if p := FolderImage(filepath.Dir(t.Path)); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			return b, strings.ToLower(filepath.Ext(p)), true
		}
	}
	return nil, "", false
}

// FolderImage finds the best cover file in dir.
func FolderImage(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var any string
	byName := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		ok := false
		for _, x := range imageExts {
			ok = ok || ext == x
		}
		if !ok {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		byName[base] = filepath.Join(dir, e.Name())
		if any == "" {
			any = filepath.Join(dir, e.Name())
		}
	}
	for _, n := range folderNames {
		if p, ok := byName[n]; ok {
			return p
		}
	}
	return any
}

// Export writes a track's cover into dir (named by Key) and returns its
// path, for hooks and notifications. Existing exports are reused.
func Export(t library.Track, dir string) string {
	key := Key(t)
	for _, x := range imageExts {
		p := filepath.Join(dir, key+x)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	data, ext, ok := Source(t)
	if !ok {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	p := filepath.Join(dir, key+ext)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return ""
	}
	return p
}

// ─── Cache ──────────────────────────────────────────────────────────────────

// Cache keeps decoded covers and rendered cells, so redrawing a frame never
// re-reads or re-scales an image.
type Cache struct {
	mu      sync.Mutex
	images  map[string]*list.Element
	order   *list.List
	cells   map[string][]string
	limit   int
	missing map[string]bool
	gen     map[string]image.Image // generated placeholders
}

type entry struct {
	key string
	img image.Image
}

// NewCache holds up to limit decoded covers.
func NewCache(limit int) *Cache {
	return &Cache{images: map[string]*list.Element{}, order: list.New(), cells: map[string][]string{}, limit: limit, missing: map[string]bool{}, gen: map[string]image.Image{}}
}

// Image returns a track's decoded cover, or nil if it has none.
func (c *Cache) Image(t library.Track) image.Image {
	key := Key(t)
	c.mu.Lock()
	if el, ok := c.images[key]; ok {
		c.order.MoveToFront(el)
		c.mu.Unlock()
		return el.Value.(*entry).img
	}
	if c.missing[key] {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	var img image.Image
	if data, _, ok := Source(t); ok {
		img, _, _ = image.Decode(bytes.NewReader(data))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if img == nil {
		c.missing[key] = true
		return nil
	}
	c.images[key] = c.order.PushFront(&entry{key, img})
	for c.order.Len() > c.limit {
		old := c.order.Remove(c.order.Back()).(*entry)
		delete(c.images, old.key)
		for k := range c.cells {
			if strings.HasPrefix(k, old.key+"|") {
				delete(c.cells, k)
			}
		}
	}
	return img
}

// Cells renders a track's cover at w×h, or a generated placeholder (seeded
// by album, tinted with colors) when it has none. With px set (the terminal
// speaks the kitty graphics protocol) the cover is drawn as real pixels.
func (c *Cache) Cells(t library.Track, w, h int, mode Mode, colors []color.Color, px *Pixels) []string {
	if px != nil {
		img := c.Image(t)
		key := Key(t)
		if img == nil {
			key = "placeholder|" + t.Album + t.Artist + "|" + colorKey(colors)
			img = c.placeholder(key, t, colors)
		}
		return px.Cells(key, img, w, h)
	}
	key := Key(t) + "|" + string(mode) + "|" + itoa(w) + "x" + itoa(h)
	c.mu.Lock()
	if cells, ok := c.cells[key]; ok {
		c.mu.Unlock()
		return cells
	}
	c.mu.Unlock()
	img := c.Image(t)
	placeholder := img == nil
	if placeholder {
		img = c.placeholder("placeholder|"+t.Album+t.Artist+"|"+colorKey(colors), t, colors)
	}
	cells := Render(img, w, h, mode)
	if !placeholder { // placeholders follow the theme; don't cache them
		c.mu.Lock()
		c.cells[key] = cells
		c.mu.Unlock()
	}
	return cells
}

// placeholder returns the generated cover for key, building it once.
func (c *Cache) placeholder(key string, t library.Track, colors []color.Color) image.Image {
	c.mu.Lock()
	defer c.mu.Unlock()
	if img, ok := c.gen[key]; ok {
		return img
	}
	if len(c.gen) > 64 {
		c.gen = map[string]image.Image{}
	}
	img := Placeholder(t.Album+t.Artist, colors)
	c.gen[key] = img
	return img
}

func colorKey(cs []color.Color) string {
	var b strings.Builder
	for _, c := range cs[:min(len(cs), 3)] {
		r, g, bb, _ := c.RGBA()
		b.WriteString(itoa(int(r>>8)) + "." + itoa(int(g>>8)) + "." + itoa(int(bb>>8)) + ";")
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// Placeholder paints a soft generative cover from the theme's colours,
// different for every album: two glowing blobs over a vertical gradient,
// with a faint ring like a record's groove.
func Placeholder(seed string, colors []color.Color) image.Image {
	const n = 96
	if len(colors) < 3 {
		colors = []color.Color{color.RGBA{40, 30, 80, 255}, color.RGBA{230, 80, 150, 255}, color.RGBA{40, 210, 230, 255}}
	}
	h := sha1.Sum([]byte(seed))
	pick := func(i int) [3]float64 {
		r, g, b, _ := colors[int(h[i])%len(colors)].RGBA()
		return [3]float64{float64(r >> 8), float64(g >> 8), float64(b >> 8)}
	}
	c1, c2, c3 := pick(0), pick(1), pick(2)
	if c2 == c1 {
		c2 = pick(3)
	}
	bx, by := 0.25+float64(h[4])/255*0.5, 0.25+float64(h[5])/255*0.5
	dx, dy := 1-bx, 1-by
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := float64(x)/n, float64(y)/n
			base := lerp3([3]float64{c1[0] * 0.25, c1[1] * 0.25, c1[2] * 0.3}, [3]float64{c1[0] * 0.5, c1[1] * 0.5, c1[2] * 0.55}, fy)
			g1 := math.Exp(-((fx-bx)*(fx-bx) + (fy-by)*(fy-by)) * 9)
			g2 := math.Exp(-((fx-dx)*(fx-dx) + (fy-dy)*(fy-dy)) * 7)
			col := lerp3(base, c2, g1*0.9)
			col = lerp3(col, c3, g2*0.75)
			r := math.Hypot(fx-0.5, fy-0.5)
			if math.Abs(r-0.36) < 0.012 {
				col = lerp3(col, [3]float64{255, 255, 255}, 0.18)
			}
			img.Set(x, y, color.RGBA{uint8(col[0]), uint8(col[1]), uint8(col[2]), 255})
		}
	}
	return img
}

func lerp3(a, b [3]float64, t float64) [3]float64 {
	t = math.Min(math.Max(t, 0), 1)
	return [3]float64{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
}

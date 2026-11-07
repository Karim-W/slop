// Package tui is a small declarative UI kit for terminal apps, in the spirit
// of SwiftUI and Flutter, built on Bubble Tea and Lip Gloss.
//
// You describe the screen as a tree of Views, rebuilt from your model on
// every frame, and tui lays it out and draws it:
//
//	func (m *Model) View() tea.View {
//		root := tui.VStack(
//			tui.HStack(tui.Text("files").Bold(), tui.Spacer(), tui.Text(m.status).Fg(tui.Muted)),
//			tui.Panel(m.list()).Title("Files").Focused(true).Grow(),
//			tui.KeyHints(tui.H("q", "quit"), tui.H("?", "help")),
//		)
//		return tea.NewView(tui.Render(m.palette, root, m.width, m.height))
//	}
//
// Layout works like Flutter's: a parent offers its children a size, each
// child reports the size it wants within that (Measure), and the parent then
// hands each child its final size to Draw into. Stacks give fixed-size
// children what they ask for and share the rest among children marked Grow
// (or Spacer), in proportion to their weight.
//
// State lives in your Bubble Tea model, not in views. Views are cheap values
// built fresh each frame, so there is nothing to keep in sync.
package tui

import (
	"strings"

	"github.com/karim-w/slop/pkg/utils/textx"
)

// Size is a width and height in terminal cells.
type Size struct{ W, H int }

// Ctx is passed down the tree while drawing.
type Ctx struct {
	// P is the palette views resolve their colours against.
	P *Palette
}

// View is one node of the UI tree.
type View interface {
	// Measure returns the size the view wants when offered avail. It must
	// not exceed avail.
	Measure(c Ctx, avail Size) Size
	// Draw renders the view into exactly size.H lines of exactly size.W
	// cells each.
	Draw(c Ctx, size Size) []string
}

// Render draws root into a w×h block and returns it as one string, ready to
// hand to tea.NewView. A nil palette means the default theme.
func Render(p *Palette, root View, w, h int) string {
	if w <= 0 || h <= 0 || root == nil {
		return ""
	}
	if p == nil {
		p = DefaultPalette()
	}
	return strings.Join(block(root.Draw(Ctx{P: p}, Size{w, h}), w, h), "\n")
}

// Draw renders v at exactly size; nil views draw as blank space.
func Draw(c Ctx, v View, size Size) []string {
	if v == nil || size.W <= 0 || size.H <= 0 {
		return block(nil, size.W, size.H)
	}
	return block(v.Draw(c, size), size.W, size.H)
}

// Measure is v.Measure with nil-safety and clamping.
func Measure(c Ctx, v View, avail Size) Size {
	if v == nil || avail.W <= 0 || avail.H <= 0 {
		return Size{}
	}
	s := v.Measure(c, avail)
	return Size{min(max(s.W, 0), avail.W), min(max(s.H, 0), avail.H)}
}

// block forces lines into exactly w×h cells.
func block(lines []string, w, h int) []string {
	if h <= 0 {
		return nil
	}
	out := make([]string, h)
	for i := range out {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = textx.Pad(l, w)
	}
	return out
}

func blank(w int) string {
	if w <= 0 {
		return ""
	}
	return strings.Repeat(" ", w)
}

// ─── Flex ───────────────────────────────────────────────────────────────────

// flexible is implemented by views that take a share of leftover space in a
// stack. Weight 0 means fixed size.
type flexible interface{ flexWeight() int }

func weightOf(v View) int {
	if f, ok := v.(flexible); ok {
		return f.flexWeight()
	}
	return 0
}

type grow struct {
	View
	weight int
}

func (g grow) flexWeight() int { return g.weight }

// Grow makes v take a share of a stack's leftover space with weight w, like
// Flutter's Expanded. Grow(1, a) next to Grow(2, b) gives b twice as much.
func Grow(w int, v View) View {
	if v == nil {
		return nil
	}
	return grow{View: v, weight: max(w, 1)}
}

type spacer struct{}

func (spacer) Measure(Ctx, Size) Size      { return Size{} }
func (spacer) Draw(_ Ctx, s Size) []string { return block(nil, s.W, s.H) }
func (spacer) flexWeight() int             { return 1 }

// Spacer is empty space that grows to push its siblings apart.
func Spacer() View { return spacer{} }

type empty struct{}

func (empty) Measure(Ctx, Size) Size      { return Size{} }
func (empty) Draw(_ Ctx, s Size) []string { return block(nil, s.W, s.H) }

// Empty draws nothing and takes no space.
func Empty() View { return empty{} }

// ─── Composition helpers ────────────────────────────────────────────────────

// If returns v when cond holds and nil otherwise. Stacks skip nil children.
func If(cond bool, v View) View {
	if cond {
		return v
	}
	return nil
}

// IfElse returns a when cond holds and b otherwise.
func IfElse(cond bool, a, b View) View {
	if cond {
		return a
	}
	return b
}

// group is a list of views that a stack splices in place.
type group []View

func (g group) Measure(c Ctx, avail Size) Size { return VStack(g...).Measure(c, avail) }
func (g group) Draw(c Ctx, s Size) []string    { return VStack(g...).Draw(c, s) }

// Group bundles views so they can be passed as one stack child. Inside a
// stack the views are spliced in as if listed directly; elsewhere they
// stack vertically.
func Group(views ...View) View { return group(views) }

// ForEach maps items to views, for use as a stack child:
//
//	tui.VStack(header, tui.ForEach(files, func(i int, f File) tui.View { … }))
func ForEach[T any](items []T, fn func(i int, item T) View) View {
	out := make(group, 0, len(items))
	for i, it := range items {
		out = append(out, fn(i, it))
	}
	return out
}

// Func is a view drawn by a function, for one-off custom drawing. The
// function receives the final size and returns the lines to show.
type Func func(c Ctx, size Size) []string

func (f Func) Measure(_ Ctx, avail Size) Size { return avail }
func (f Func) Draw(c Ctx, s Size) []string    { return f(c, s) }
func (f Func) flexWeight() int                { return 1 }

// Lazy defers building a view until it is drawn, with the final size known.
// Use it when a subtree's content depends on how much room it gets.
func Lazy(build func(size Size) View) View {
	return Func(func(c Ctx, s Size) []string { return Draw(c, build(s), s) })
}

package tui

import "strings"

type axis int

const (
	vertical axis = iota
	horizontal
)

// StackView lays children out in a column (VStack) or a row (HStack).
//
// Children are stretched across the stack: every child in a VStack gets the
// stack's full width, every child in an HStack its full height. Along the
// stack, fixed children get the size they measure and grown children (Grow,
// Spacer) share whatever is left.
type StackView struct {
	axis     axis
	children []View
	gap      int
	weight   int
}

// VStack stacks children top to bottom. Nil children are skipped.
func VStack(children ...View) StackView {
	return StackView{axis: vertical, children: flatten(children)}
}

// HStack places children left to right. Nil children are skipped.
func HStack(children ...View) StackView {
	return StackView{axis: horizontal, children: flatten(children)}
}

// Gap puts n blank cells between children.
func (s StackView) Gap(n int) StackView { s.gap = max(n, 0); return s }

// Grow makes the whole stack take leftover space in its parent stack.
func (s StackView) Grow() StackView { s.weight = 1; return s }

func (s StackView) flexWeight() int { return s.weight }

func flatten(in []View) []View {
	out := make([]View, 0, len(in))
	for _, v := range in {
		switch v := v.(type) {
		case nil:
		case group:
			out = append(out, flatten(v)...)
		default:
			out = append(out, v)
		}
	}
	return out
}

// main and cross pick the stack's axis out of a Size.
func (s StackView) main(z Size) int {
	if s.axis == vertical {
		return z.H
	}
	return z.W
}

func (s StackView) cross(z Size) int {
	if s.axis == vertical {
		return z.W
	}
	return z.H
}

func (s StackView) size(main, cross int) Size {
	if s.axis == vertical {
		return Size{W: cross, H: main}
	}
	return Size{W: main, H: cross}
}

func (s StackView) Measure(c Ctx, avail Size) Size {
	left := s.main(avail)
	used, cross, grows := 0, 0, false
	for i, ch := range s.children {
		if i > 0 {
			used += s.gap
		}
		if weightOf(ch) > 0 {
			grows = true
			z := Measure(c, ch, s.size(max(left-used, 0), s.cross(avail)))
			cross = max(cross, s.cross(z))
			continue
		}
		z := Measure(c, ch, s.size(max(left-used, 0), s.cross(avail)))
		used += s.main(z)
		cross = max(cross, s.cross(z))
	}
	if grows {
		used = left
	}
	return s.size(min(used, left), min(cross, s.cross(avail)))
}

// sizes decides each child's length along the stack.
func (s StackView) sizes(c Ctx, total, cross int) []int {
	n := len(s.children)
	out := make([]int, n)
	left := total - s.gap*max(n-1, 0)
	weights := 0
	for i, ch := range s.children {
		if w := weightOf(ch); w > 0 {
			weights += w
			continue
		}
		z := Measure(c, ch, s.size(max(left, 0), cross))
		out[i] = min(s.main(z), max(left, 0))
		left -= out[i]
	}
	if weights == 0 || left <= 0 {
		return out
	}
	share, last := left, -1
	for i, ch := range s.children {
		if w := weightOf(ch); w > 0 {
			out[i] = share * w / weights
			left -= out[i]
			last = i
		}
	}
	out[last] += left // rounding remainder
	return out
}

func (s StackView) Draw(c Ctx, size Size) []string {
	cross := s.cross(size)
	lens := s.sizes(c, s.main(size), cross)
	if s.axis == vertical {
		out := make([]string, 0, size.H)
		for i, ch := range s.children {
			if i > 0 && s.gap > 0 {
				out = append(out, block(nil, size.W, s.gap)...)
			}
			out = append(out, Draw(c, ch, Size{size.W, lens[i]})...)
		}
		return block(out, size.W, size.H)
	}
	rows := make([]strings.Builder, size.H)
	for i, ch := range s.children {
		if i > 0 && s.gap > 0 {
			for r := range rows {
				rows[r].WriteString(blank(s.gap))
			}
		}
		if lens[i] <= 0 {
			continue
		}
		lines := Draw(c, ch, Size{lens[i], size.H})
		for r := range rows {
			rows[r].WriteString(lines[r])
		}
	}
	out := make([]string, size.H)
	for r := range rows {
		out[r] = rows[r].String()
	}
	return block(out, size.W, size.H)
}

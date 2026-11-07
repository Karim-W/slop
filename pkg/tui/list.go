package tui

// Scroller decides which row a list shows first. Lists call Window while
// drawing. Scroll is the plain implementation; anim.Scroller animates.
type Scroller interface {
	Window(cursor, rows, total int) int
}

// Scroll is the scroll position of a list. Keep one in your model per list;
// the list updates it while drawing so the cursor stays visible.
type Scroll struct{ Offset int }

// Window implements Scroller: follow the cursor, then report the offset.
func (s *Scroll) Window(cursor, rows, total int) int {
	s.Follow(cursor, rows, total)
	return s.Offset
}

// Follow moves the offset the least amount needed to show cursor in a
// window of rows over total items.
func (s *Scroll) Follow(cursor, rows, total int) {
	if rows <= 0 {
		return
	}
	if cursor < s.Offset {
		s.Offset = cursor
	}
	if cursor >= s.Offset+rows {
		s.Offset = cursor - rows + 1
	}
	s.Offset = max(min(s.Offset, total-rows), 0)
}

// ListView is a virtualised, scrolling list: only visible rows are built.
// It grows to fill the space a stack gives it.
type ListView struct {
	count     int
	cursor    int
	scroll    Scroller
	row       func(i int, selected bool) View
	highlight Color
	empty     View
	rowHeight int
}

// List shows count rows built by row. cursor is the selected index (-1 for
// none); scroll remembers the offset between frames (nil starts at the top).
func List(count, cursor int, scroll Scroller, row func(i int, selected bool) View) ListView {
	return ListView{count: count, cursor: cursor, scroll: scroll, row: row, rowHeight: 1}
}

// Highlight paints the selected row's background, such as Selection.
func (l ListView) Highlight(c Color) ListView { l.highlight = c; return l }

// Empty is shown when the list has no rows.
func (l ListView) Empty(v View) ListView { l.empty = v; return l }

// RowHeight sets how many lines each row takes (default 1).
func (l ListView) RowHeight(h int) ListView { l.rowHeight = max(h, 1); return l }

func (l ListView) flexWeight() int { return 1 }

func (l ListView) Measure(_ Ctx, avail Size) Size {
	return Size{avail.W, min(avail.H, max(l.count*l.rowHeight, 1))}
}

func (l ListView) Draw(c Ctx, size Size) []string {
	if l.count == 0 {
		return Draw(c, l.empty, size)
	}
	rows := max(size.H/l.rowHeight, 1)
	off := 0
	if l.scroll != nil {
		off = l.scroll.Window(l.cursor, rows, l.count)
	}
	off = max(min(off, l.count-rows), 0)
	out := make([]string, 0, size.H)
	for i := off; i < l.count && len(out) < size.H; i++ {
		sel := i == l.cursor
		lines := Draw(c, l.row(i, sel), Size{size.W, l.rowHeight})
		if sel && l.highlight != nil {
			bg := resolve(l.highlight, c.P)
			for j := range lines {
				lines[j] = Highlight(lines[j], bg)
			}
		}
		out = append(out, lines...)
	}
	return block(out, size.W, size.H)
}

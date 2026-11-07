// Package graph lays out a commit DAG into terminal lanes.
//
// Every commit occupies exactly one row. Each lane is two cells wide: a glyph
// cell and a connector cell to its right. Branch-outs, merges and convergences
// are drawn on the commit's own row with rounded box-drawing characters, which
// keeps the graph dense while staying readable.
package graph

// Node is a placeholder glyph replaced by the renderer (HEAD, merge, etc.).
const Node = '•'

type Cell struct {
	Glyph     rune
	Color     int // lane colour index for Glyph
	Conn      rune
	ConnColor int
}

type Row struct {
	Cells   []Cell
	NodeCol int
	Color   int // colour of the commit's lane
	// Below reports, per lane, whether a line continues from this row toward
	// the next (older) row, and in which colour. Horizontal renderers use it
	// to draw the segments between commit columns.
	Below      []bool
	BelowColor []int
}

// Build computes rows for commits given in topological order. parents[i]
// lists the parent ids of ids[i].
func Build(ids []string, parents [][]string) []Row {
	var lanes []string
	var colors []int
	next := 0
	alloc := func(h string) int {
		for i, l := range lanes {
			if l == "" {
				lanes[i], colors[i] = h, next
				next++
				return i
			}
		}
		lanes = append(lanes, h)
		colors = append(colors, next)
		next++
		return len(lanes) - 1
	}
	find := func(h string) int {
		for i, l := range lanes {
			if l == h {
				return i
			}
		}
		return -1
	}

	rows := make([]Row, len(ids))
	for i, id := range ids {
		col := find(id)
		tip := col == -1
		if tip {
			col = alloc(id)
		}
		above := make([]bool, len(lanes))
		for j, l := range lanes {
			above[j] = l != "" && !(tip && j == col)
		}
		var joins []int
		for j, l := range lanes {
			if j != col && l == id {
				joins = append(joins, j)
			}
		}

		ps := parents[i]
		var forks, links []int
		if len(ps) == 0 {
			lanes[col] = ""
		} else {
			lanes[col] = ps[0]
			for _, p := range ps[1:] {
				if p == ps[0] {
					continue
				}
				k := find(p)
				switch {
				case k == -1:
					forks = append(forks, alloc(p))
				case k != col && !contains(joins, k):
					links = append(links, k)
				}
			}
		}
		joinColors := map[int]int{}
		for _, j := range joins {
			joinColors[j] = colors[j]
			lanes[j] = ""
		}

		n := len(lanes)
		if len(above) > n {
			n = len(above)
		}
		lo, hi := col, col
		for _, set := range [][]int{joins, forks, links} {
			for _, j := range set {
				lo, hi = min(lo, j), max(hi, j)
			}
		}
		colorAt := func(j int) int {
			if c, ok := joinColors[j]; ok {
				return c
			}
			if j < len(colors) {
				return colors[j]
			}
			return 0
		}
		isEnd := func(j int) bool { return contains(joins, j) || contains(forks, j) || contains(links, j) }
		// horizontal colour of the segment j..j+1 is the colour of the nearest
		// endpoint further away from the node.
		hcolor := func(j int) int {
			if j >= col {
				for e := j + 1; e <= hi; e++ {
					if isEnd(e) {
						return colorAt(e)
					}
				}
			} else {
				for e := j; e >= lo; e-- {
					if isEnd(e) {
						return colorAt(e)
					}
				}
			}
			return colorAt(col)
		}

		cells := make([]Cell, n)
		for j := 0; j < n; j++ {
			c := Cell{Glyph: ' ', Conn: ' ', Color: colorAt(j)}
			inner := (j > col && j < hi) || (j < col && j > lo)
			switch {
			case j == col:
				c.Glyph = Node
			case contains(joins, j):
				c.Glyph = pick(inner, '┴', j > col, '╯', '╰')
			case contains(forks, j):
				c.Glyph = pick(inner, '┬', j > col, '╮', '╭')
			case contains(links, j):
				c.Glyph = pick(inner, '┼', j > col, '┤', '├')
			case j < len(above) && above[j] && j < len(lanes) && lanes[j] != "":
				if j > lo && j < hi {
					c.Glyph = '┼'
				} else {
					c.Glyph = '│'
				}
			case j > lo && j < hi:
				c.Glyph = '─'
				c.Color = hcolor(j)
			}
			if j >= lo && j < hi {
				c.Conn = '─'
				c.ConnColor = hcolor(j)
			}
			cells[j] = c
		}
		below := make([]bool, len(lanes))
		belowC := make([]int, len(lanes))
		for j, l := range lanes {
			below[j] = l != ""
			belowC[j] = colors[j]
		}
		rows[i] = Row{Cells: cells, NodeCol: col, Color: colorAt(col), Below: below, BelowColor: belowC}

		for len(lanes) > 0 && lanes[len(lanes)-1] == "" {
			lanes = lanes[:len(lanes)-1]
			colors = colors[:len(colors)-1]
		}
	}
	// trim trailing blank cells
	for i := range rows {
		c := rows[i].Cells
		for len(c) > 0 && c[len(c)-1].Glyph == ' ' && c[len(c)-1].Conn == ' ' {
			c = c[:len(c)-1]
		}
		rows[i].Cells = c
	}
	return rows
}

func pick(inner bool, t rune, right bool, r, l rune) rune {
	if inner {
		return t
	}
	if right {
		return r
	}
	return l
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Horizontal maps a glyph from the vertical layout (time flowing downward) to
// the horizontal layout (time flowing left to right, lanes stacked as rows).
// Up becomes right, down becomes left, left becomes up and right becomes down.
func Horizontal(r rune) rune {
	switch r {
	case '│':
		return '─'
	case '─':
		return '│'
	case '╯':
		return '╰'
	case '╰':
		return '╭'
	case '╮':
		return '╯'
	case '╭':
		return '╮'
	case '┴':
		return '├'
	case '┬':
		return '┤'
	case '┤':
		return '┴'
	case '├':
		return '┬'
	}
	return r
}

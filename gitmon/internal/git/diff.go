package git

import (
	"strconv"
	"strings"
)

// LineKind classifies a rendered diff line.
type LineKind int

const (
	LineContext LineKind = iota
	LineAdd
	LineDel
	LineHunk
	LineMeta
	LineNoNewline
)

type DiffLine struct {
	Kind LineKind
	Text string // without the leading +/-/space marker
	Old  int    // old line number (0 if n/a)
	New  int    // new line number (0 if n/a)
	Hunk int    // index of the hunk this line belongs to (-1 for meta)
	Raw  string
}

type Hunk struct {
	Header string
	Start  int // index into Lines of the hunk header
	End    int // exclusive
}

// Diff is a parsed single- or multi-file patch.
type Diff struct {
	Lines  []DiffLine
	Hunks  []Hunk
	Header []string // file header lines (for the first file) used to build patches
	Binary bool
	Adds   int
	Dels   int
}

// ParseDiff parses unified diff output.
func ParseDiff(text string) *Diff {
	d := &Diff{}
	if text == "" {
		return d
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	inHunk := false
	oldN, newN := 0, 0
	hunk := -1
	headerDone := false
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "diff --git"):
			inHunk = false
			if len(d.Header) > 0 {
				headerDone = true
			}
			if !headerDone {
				d.Header = append(d.Header, l)
			}
			d.Lines = append(d.Lines, DiffLine{Kind: LineMeta, Text: l, Hunk: -1, Raw: l})
		case strings.HasPrefix(l, "@@"):
			inHunk = true
			headerDone = true
			oldN, newN = parseHunkHeader(l)
			if hunk >= 0 {
				d.Hunks[hunk].End = len(d.Lines)
			}
			hunk++
			d.Hunks = append(d.Hunks, Hunk{Header: l, Start: len(d.Lines)})
			d.Lines = append(d.Lines, DiffLine{Kind: LineHunk, Text: l, Hunk: hunk, Raw: l})
		case !inHunk:
			if strings.HasPrefix(l, "Binary files") {
				d.Binary = true
			}
			if !headerDone {
				d.Header = append(d.Header, l)
			}
			d.Lines = append(d.Lines, DiffLine{Kind: LineMeta, Text: l, Hunk: -1, Raw: l})
		case strings.HasPrefix(l, "+"):
			d.Lines = append(d.Lines, DiffLine{Kind: LineAdd, Text: l[1:], New: newN, Hunk: hunk, Raw: l})
			newN++
			d.Adds++
		case strings.HasPrefix(l, "-"):
			d.Lines = append(d.Lines, DiffLine{Kind: LineDel, Text: l[1:], Old: oldN, Hunk: hunk, Raw: l})
			oldN++
			d.Dels++
		case strings.HasPrefix(l, `\`):
			d.Lines = append(d.Lines, DiffLine{Kind: LineNoNewline, Text: l, Hunk: hunk, Raw: l})
		default:
			t := l
			if len(t) > 0 {
				t = t[1:]
			}
			d.Lines = append(d.Lines, DiffLine{Kind: LineContext, Text: t, Old: oldN, New: newN, Hunk: hunk, Raw: l})
			oldN++
			newN++
		}
	}
	if hunk >= 0 {
		d.Hunks[hunk].End = len(d.Lines)
	}
	return d
}

func parseHunkHeader(h string) (int, int) {
	// @@ -a,b +c,d @@
	f := strings.Fields(h)
	if len(f) < 3 {
		return 0, 0
	}
	num := func(s string) int {
		s = strings.TrimLeft(s, "-+")
		s, _, _ = strings.Cut(s, ",")
		n, _ := strconv.Atoi(s)
		return n
	}
	return num(f[1]), num(f[2])
}

// HunkPatch builds an applicable patch containing only hunk i.
func (d *Diff) HunkPatch(i int) string {
	if i < 0 || i >= len(d.Hunks) {
		return ""
	}
	var b strings.Builder
	for _, h := range d.Header {
		b.WriteString(h)
		b.WriteByte('\n')
	}
	h := d.Hunks[i]
	for _, l := range d.Lines[h.Start:h.End] {
		b.WriteString(l.Raw)
		b.WriteByte('\n')
	}
	return b.String()
}

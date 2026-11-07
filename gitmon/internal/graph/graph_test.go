package graph

import (
	"strings"
	"testing"
)

func render(rows []Row) string {
	var b strings.Builder
	for _, r := range rows {
		for _, c := range r.Cells {
			g := c.Glyph
			if g == Node {
				g = '*'
			}
			b.WriteRune(g)
			b.WriteRune(c.Conn)
		}
		b.WriteString("|\n")
	}
	return b.String()
}

func TestLinear(t *testing.T) {
	rows := Build([]string{"c", "b", "a"}, [][]string{{"b"}, {"a"}, nil})
	want := "* |\n* |\n* |\n"
	if got := render(rows); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestBranchAndMerge(t *testing.T) {
	// m merges f into main; f and b both descend from a.
	ids := []string{"m", "f", "b", "a"}
	parents := [][]string{{"b", "f"}, {"a"}, {"a"}, nil}
	got := render(Build(ids, parents))
	want := "" +
		"*─╮ |\n" + // merge forks a lane for f
		"│ * |\n" + // f on lane 1
		"* │ |\n" + // b on lane 0
		"*─╯ |\n" // a: lane 1 converges
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestLaneColorsStable(t *testing.T) {
	ids := []string{"m", "f", "b", "a"}
	parents := [][]string{{"b", "f"}, {"a"}, {"a"}, nil}
	rows := Build(ids, parents)
	if rows[1].Color == rows[2].Color {
		t.Fatalf("branch and mainline should get different colours")
	}
	if rows[0].Color != rows[2].Color {
		t.Fatalf("mainline colour should persist down its lane")
	}
}

package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func plain(t *testing.T, v View, w, h int) []string {
	t.Helper()
	out := strings.Split(ansi.Strip(Render(nil, v, w, h)), "\n")
	if len(out) != h {
		t.Fatalf("got %d lines, want %d", len(out), h)
	}
	for i, l := range out {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("line %d is %d cells, want %d: %q", i, got, w, l)
		}
	}
	return out
}

func TestHStackSpacerPushesApart(t *testing.T) {
	out := plain(t, HStack(Text("left"), Spacer(), Text("right")), 20, 1)
	if out[0] != "left           right" {
		t.Fatalf("%q", out[0])
	}
}

func TestVStackSharesLeftoverByWeight(t *testing.T) {
	v := VStack(
		Text("head"),
		Grow(1, Text("a")),
		Grow(2, Text("b")),
		Text("foot"),
	)
	out := plain(t, v, 6, 8) // 6 left over: a gets 2, b gets 4
	want := []string{"head", "a", "", "b", "", "", "", "foot"}
	for i := range want {
		if strings.TrimRight(out[i], " ") != want[i] {
			t.Fatalf("line %d = %q, want %q\n%s", i, out[i], want[i], strings.Join(out, "\n"))
		}
	}
}

func TestPanelChrome(t *testing.T) {
	out := plain(t, Panel(Text("body")).Title("Files").Label(Text("3 items")), 30, 4)
	if !strings.HasPrefix(out[0], "╭─ Files ") || !strings.HasSuffix(out[0], " 3 items ─╮") {
		t.Fatalf("top = %q", out[0])
	}
	if out[1] != "│body                        │" {
		t.Fatalf("body = %q", out[1])
	}
	if !strings.HasPrefix(out[3], "╰") || !strings.HasSuffix(out[3], "╯") {
		t.Fatalf("bottom = %q", out[3])
	}
}

func TestListFollowsCursor(t *testing.T) {
	sc := &Scroll{}
	items := []string{"a", "b", "c", "d", "e"}
	l := List(len(items), 4, sc, func(i int, sel bool) View {
		if sel {
			return Text("> " + items[i])
		}
		return Text("  " + items[i])
	})
	out := plain(t, l, 5, 3)
	if sc.Offset != 2 || strings.TrimSpace(out[2]) != "> e" {
		t.Fatalf("offset %d, %q", sc.Offset, out)
	}
}

func TestModalCentresDialog(t *testing.T) {
	base := VStack(ForEach(make([]int, 9), func(int, int) View { return Text(strings.Repeat(".", 30)) }))
	out := plain(t, Modal(base, Frame(10, 3, Panel(Empty()))), 30, 9)
	if !strings.Contains(out[3], "..........╭────────╮..........") {
		t.Fatalf("%s", strings.Join(out, "\n"))
	}
}

func TestKeyHintsDropWhatDoesNotFit(t *testing.T) {
	out := plain(t, KeyHints(H("q", "quit"), H("?", "help")), 12, 1)
	if strings.TrimSpace(out[0]) != "q quit" {
		t.Fatalf("%q", out[0])
	}
}

func TestWrapAndAlign(t *testing.T) {
	out := plain(t, Text("one two three").Wrap().Align(End), 8, 2)
	if out[0] != " one two" || out[1] != "   three" {
		t.Fatalf("%q", out)
	}
}

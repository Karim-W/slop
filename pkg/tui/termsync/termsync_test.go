package termsync

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/pkg/domain/theme"
)

func TestParseOSC4(t *testing.T) {
	i, c, ok := ParseOSC4("\x1b]4;5;rgb:c7c7/9292/eaea\x1b\\")
	if !ok || i != 5 || theme.ToHex(c) != "#c792ea" {
		t.Fatalf("%d %v %v", i, c, ok)
	}
	if _, _, ok := ParseOSC4("\x1b]4;99;rgb:0/0/0\x07"); ok {
		t.Fatal("index out of range should fail")
	}
}

// Quitting while colour replies are in flight must wait for them, or they
// leak into the shell.
func TestGateHoldsQuitUntilReplies(t *testing.T) {
	var g Gate
	g.Query()
	if cmd := g.WhenQuiet(tea.Quit); cmd == nil {
		t.Fatal("expected a timeout tick")
	} else if _, ok := cmd().(tea.QuitMsg); ok {
		t.Fatal("quit ran while replies were pending")
	}
	var last tea.Cmd
	for i := 0; i < Replies; i++ {
		last = g.Reply()
	}
	if last == nil {
		t.Fatal("last reply should release quit")
	}
	if _, ok := last().(tea.QuitMsg); !ok {
		t.Fatal("expected quit after the last reply")
	}
	if cmd := g.WhenQuiet(tea.Quit); cmd == nil {
		t.Fatal("quiet terminal should quit at once")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected immediate quit")
	}
}

func TestGateExpectAddsReplies(t *testing.T) {
	var g Gate
	g.Query()
	g.Expect(2)
	if g.Pending() != Replies+2 {
		t.Fatalf("pending %d", g.Pending())
	}
	g.WhenQuiet(tea.Quit)
	var last tea.Cmd
	for i := 0; i < Replies+2; i++ {
		last = g.Reply()
	}
	if last == nil {
		t.Fatal("quit should run after every reply, including the extra ones")
	}
}

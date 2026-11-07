package ui

import (
	"fmt"
	"image/color"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/karim-w/gitmon/internal/git"
)

// drive executes a command tree synchronously, skipping timers.
func drive(m *Model, cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 8 {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(300 * time.Millisecond):
		return
	}
	if msg == nil {
		return
	}
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			drive(m, c, depth+1)
		}
		return
	}
	if reflect.TypeOf(msg).Name() == "sequenceMsg" {
		v := reflect.ValueOf(msg)
		for i := 0; i < v.Len(); i++ {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
				drive(m, c, depth+1)
			}
		}
		return
	}
	switch msg.(type) {
	case frameMsg, pollMsg, toastExpireMsg, themeTimeoutMsg, tea.RawMsg:
		return
	}
	_, next := m.Update(msg)
	drive(m, next, depth+1)
}

func settle(m *Model) {
	for i := 0; i < 400; i++ {
		m.stepAnimations()
	}
}

func snap(t *testing.T, m *Model, name string) {
	settle(m)
	v := m.View()
	out := v.Content
	if os.Getenv("SNAP_ANSI") == "" {
		out = ansi.Strip(out)
	}
	fmt.Printf("\n===== %s =====\n%s\n", name, out)
	lines := strings.Split(ansi.Strip(v.Content), "\n")
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > m.w {
			t.Errorf("%s: line %d too wide: %d > %d", name, i, w, m.w)
		}
	}
	if len(lines) > m.h {
		t.Errorf("%s: %d lines > height %d", name, len(lines), m.h)
	}
}

func key(m *Model, k string) {
	var msg tea.KeyPressMsg
	switch k {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	case "left":
		msg = tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		msg = tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		msg = tea.KeyPressMsg{Code: ' ', Text: " "}
	case "ctrl+p":
		msg = tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	default:
		msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
	_, cmd := m.Update(msg)
	drive(m, cmd, 0)
}

func newTestModel(t *testing.T) *Model {
	path := os.Getenv("GITMON_DEMO")
	if path == "" {
		t.Skip("set GITMON_DEMO")
	}
	repo, err := git.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	th := os.Getenv("GITMON_THEME")
	if th == "" {
		th = "nebula"
	}
	m := New(repo, th)
	w, h := 130, 36
	fmt.Sscanf(os.Getenv("SIZE"), "%dx%d", &w, &h)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	drive(m, m.Init(), 0)
	return m
}

func TestSnapshot(t *testing.T) {
	m := newTestModel(t)
	snap(t, m, "start")
	for _, k := range strings.Split(os.Getenv("KEYS"), ",") {
		switch k {
		case "":
		case "|":
			snap(t, m, "step")
		default:
			key(m, k)
		}
	}
	if os.Getenv("KEYS") != "" {
		snap(t, m, "after keys")
	}
}

// TestTerminalTheme simulates Rex answering the OSC colour queries.
func TestTerminalTheme(t *testing.T) {
	os.Setenv("GITMON_THEME", "rex")
	defer os.Unsetenv("GITMON_THEME")
	m := newTestModel(t)
	if !m.termMode || !m.awaitTheme {
		t.Fatalf("expected terminal mode awaiting colours")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.RGBA{0x01, 0x16, 0x27, 0xff}})
	m.Update(tea.ForegroundColorMsg{Color: color.RGBA{0xd6, 0xde, 0xeb, 0xff}})
	m.Update(uv.UnknownOscEvent("\x1b]4;5;rgb:c7c7/9292/eaea\x1b\\"))
	m.Update(uv.UnknownOscEvent("\x1b]4;2;rgb:2222/dada/6e6e\x07"))
	if m.awaitTheme {
		t.Fatal("still awaiting theme")
	}
	if got := m.st.T.Bg; got != "#011627" {
		t.Fatalf("bg = %s", got)
	}
	if got := m.st.T.Accent; got != "#c792ea" {
		t.Fatalf("accent = %s, want magenta from palette", got)
	}
	if got := m.st.T.Success; got != "#22da6e" {
		t.Fatalf("success = %s", got)
	}
	if v := m.View(); v.BackgroundColor != nil {
		t.Fatal("terminal mode must not override the host background")
	}
}

// TestQuitWaitsForColourReplies: quitting while OSC answers are in flight
// must wait for them, or they leak into the shell.
func TestQuitWaitsForColourReplies(t *testing.T) {
	m := &Model{}
	m.queryTerm()
	if cmd := m.whenQuiet(tea.Quit); cmd == nil {
		t.Fatal("expected a timeout tick")
	} else if _, ok := cmd().(tea.QuitMsg); ok {
		t.Fatal("quit ran while replies were pending")
	}
	var last tea.Cmd
	for i := 0; i < oscReplies; i++ {
		last = m.gotOSCReply()
	}
	if last == nil {
		t.Fatal("last reply should release quit")
	}
	if _, ok := last().(tea.QuitMsg); !ok {
		t.Fatal("expected quit after the last reply")
	}
	if cmd := m.whenQuiet(tea.Quit); cmd == nil {
		t.Fatal("quiet terminal should quit at once")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected immediate quit")
	}
}

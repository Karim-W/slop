package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/karim-w/slop/apps/slash/internal/config"
	"github.com/karim-w/slop/apps/slash/internal/sftptest"
)

// drive runs a command tree synchronously, feeding results back into the
// model. Timers that only repaint (ticks, spinners, flash expiry) are dropped.
func drive(t *testing.T, m *Model, cmd tea.Cmd, depth int) {
	t.Helper()
	if cmd == nil || depth > 12 {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(time.Second):
		return
	}
	switch msg := msg.(type) {
	case nil, tickMsg, spinner.TickMsg, clearFlashMsg, tea.QuitMsg:
		return
	case tea.BatchMsg:
		for _, c := range msg {
			drive(t, m, c, depth+1)
		}
		return
	}
	if reflect.TypeOf(msg).Kind() == reflect.Slice { // tea.Sequence
		v := reflect.ValueOf(msg)
		for i := 0; i < v.Len(); i++ {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
				drive(t, m, c, depth+1)
			}
		}
		return
	}
	_, next := m.Update(msg)
	drive(t, m, next, depth+1)
}

func press(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		default:
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		_, cmd := m.Update(msg)
		drive(t, m, cmd, 0)
	}
}

func typeText(t *testing.T, m *Model, s string) {
	for _, r := range s {
		_, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		drive(t, m, cmd, 0)
	}
}

// screen renders the model and checks it fills the terminal exactly.
func screen(t *testing.T, m *Model) string {
	t.Helper()
	out := ansi.Strip(m.View().Content)
	lines := strings.Split(out, "\n")
	if len(lines) != m.height {
		t.Fatalf("%d lines, want %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.width {
			t.Fatalf("line %d is %d cells, want %d:\n%s", i, w, m.width, out)
		}
	}
	if testing.Verbose() {
		t.Log("\n" + out)
	}
	return out
}

func mustContain(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Fatalf("screen lacks %q:\n%s", w, out)
		}
	}
}

func TestConnectBrowseAndDialogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	os.WriteFile(filepath.Join(home, "notes.txt"), []byte("hello"), 0o644)
	os.Mkdir(filepath.Join(home, "photos"), 0o755)

	server, err := sftptest.Start("127.0.0.1:0", "dev", "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	store, _ := config.Load(filepath.Join(home, "sites.json"))
	store.Upsert(config.Site{Name: "devbox", Host: server.Host, Port: server.Port, User: "dev", Connections: 2}.WithDefaults())

	m := New(store, "nebula")
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	drive(t, m, m.Init(), 0)

	out := screen(t, m)
	mustContain(t, out, "Site Manager", "devbox", "dev@127.0.0.1", "⟋ slash", "○ offline")

	press(t, m, "enter") // connect → unknown host
	mustContain(t, screen(t, m), "Unknown host", "Fingerprint")

	press(t, m, "y") // trust → key/agent auth fails → password prompt
	mustContain(t, screen(t, m), "Password for dev@127.0.0.1")

	typeText(t, m, "pw")
	press(t, m, "enter")
	if m.session == nil {
		t.Fatalf("not connected:\n%s", screen(t, m))
	}
	out = screen(t, m)
	mustContain(t, out, "● dev@127.0.0.1", "notes.txt", "photos/", "2 connections")

	press(t, m, "?")
	mustContain(t, screen(t, m), "Navigate", "Session", "press any key to close")
	press(t, m, "q") // any key closes help
	if m.modal != nil {
		t.Fatal("help should close on any key")
	}

	press(t, m, "t")
	mustContain(t, screen(t, m), "No transfers yet")

	if dir := os.Getenv("SCREENSHOT_DIR"); dir != "" { // scripts/screenshots.sh
		press(t, m, "down", "space")
		os.WriteFile(filepath.Join(dir, "slash.ansi"), []byte(m.View().Content), 0o644)
	}

	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	mustContain(t, screen(t, m), "Terminal too small")
}

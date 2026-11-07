// Package termsync lets an app follow the host terminal's colour scheme.
//
// The app asks the terminal for its foreground, background and 16 ANSI
// colours (OSC 10, 11 and 4). The answers arrive as messages and are fed to
// theme.FromTerminal to build a matching theme. Rex and most modern
// terminals answer these queries.
//
// Answers arrive on stdin. If the app quits, or hands the terminal to an
// editor, while answers are still in flight, they land in the shell as text
// like "4;1;rgb:ffff/7b7b/7272". Gate prevents that by holding such commands
// until every answer is in, with a timeout for terminals that never answer.
package termsync

import (
	"image/color"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Replies is how many answers one Query produces: fg, bg and 16 palette entries.
const Replies = 18

// InRex reports whether the app runs inside a Rex terminal.
func InRex() bool { return os.Getenv("REX_SESSION") != "" || os.Getenv("REX_SERVER") != "" }

// ThemeName is the name to give the theme mirrored from the terminal.
func ThemeName() string {
	if InRex() {
		return "rex"
	}
	return "terminal"
}

// Query is the escape sequence that asks for the terminal's colours.
func Query() string {
	var b strings.Builder
	b.WriteString("\x1b]10;?\x1b\\\x1b]11;?\x1b\\")
	for i := 0; i < 16; i++ {
		b.WriteString("\x1b]4;" + strconv.Itoa(i) + ";?\x1b\\")
	}
	return b.String()
}

// ParseOSC4 parses a palette reply such as "\x1b]4;1;rgb:ffff/0000/0000\x1b\\".
func ParseOSC4(s string) (int, color.Color, bool) {
	s = strings.TrimPrefix(s, "\x1b]")
	s = strings.TrimPrefix(s, "\x9d")
	s = strings.TrimSuffix(s, "\x1b\\")
	s = strings.TrimSuffix(s, "\x07")
	s = strings.TrimSuffix(s, "\x9c")
	if !strings.HasPrefix(s, "4;") {
		return 0, nil, false
	}
	parts := strings.SplitN(s[2:], ";", 2)
	if len(parts) != 2 {
		return 0, nil, false
	}
	idx, err := strconv.Atoi(parts[0])
	if err != nil || idx < 0 || idx > 15 {
		return 0, nil, false
	}
	c := ansi.XParseColor(parts[1])
	if c == nil {
		return 0, nil, false
	}
	return idx, c, true
}

// FlushMsg tells the app to call Gate.Flush: the reply timeout has passed.
type FlushMsg struct{}

// Gate counts outstanding colour replies and holds commands that must not
// run while replies are in flight (quitting, running an editor).
type Gate struct {
	pending  int
	sent     time.Time
	deferred []tea.Cmd
	// Timeout is how long to wait for a terminal that never answers.
	// Zero means two seconds.
	Timeout time.Duration
}

func (g *Gate) timeout() time.Duration {
	if g.Timeout <= 0 {
		return 2 * time.Second
	}
	return g.Timeout
}

// Query sends the colour queries, unless a held command is about to run.
func (g *Gate) Query() tea.Cmd {
	if len(g.deferred) > 0 {
		return nil
	}
	g.pending = Replies
	g.sent = time.Now()
	return tea.Raw(Query())
}

// Expect counts n more replies the app asked for itself (other terminal
// queries), so quitting also waits for them.
func (g *Gate) Expect(n int) {
	if g.pending == 0 {
		g.sent = time.Now()
	}
	g.pending += n
}

// Pending is the number of replies still expected.
func (g *Gate) Pending() int { return g.pending }

// Reply counts one answer and releases held commands after the last one.
func (g *Gate) Reply() tea.Cmd {
	if g.pending > 0 {
		g.pending--
	}
	if g.pending == 0 && len(g.deferred) > 0 {
		return g.Flush()
	}
	return nil
}

// WhenQuiet runs cmd at once if no replies are in flight, or holds it until
// they are all in.
func (g *Gate) WhenQuiet(cmd tea.Cmd) tea.Cmd {
	if g.pending == 0 || time.Since(g.sent) > g.timeout() {
		g.pending = 0
		return cmd
	}
	g.deferred = append(g.deferred, cmd)
	if len(g.deferred) > 1 {
		return nil
	}
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return FlushMsg{} })
}

// Flush releases every held command.
func (g *Gate) Flush() tea.Cmd {
	cmds := g.deferred
	g.deferred = nil
	g.pending = 0
	return tea.Batch(cmds...)
}

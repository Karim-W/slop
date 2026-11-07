package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/gitmon/internal/theme"
)

// Live theme sync asks the terminal for its colours (OSC 10, 11 and 4×16).
// The answers arrive on stdin. If gitmon quits, or hands the terminal to an
// editor, while answers are still in flight, they land in the shell or the
// editor as text like "4;1;rgb:ffff/7b7b/7272". To prevent that, gitmon
// counts the outstanding answers and holds quit and terminal hand-offs until
// they are all in, with a short timeout for terminals that never answer.

const oscReplies = 18 // fg, bg and 16 palette entries

type flushDeferredMsg struct{}

// queryTerm sends the colour queries, unless gitmon is about to quit or
// hand off the terminal.
func (m *Model) queryTerm() tea.Cmd {
	if len(m.deferred) > 0 {
		return nil
	}
	m.oscPending = oscReplies
	m.oscSent = time.Now()
	return tea.Raw(theme.QuerySequence())
}

// gotOSCReply counts one answer and releases held commands once the last
// one is in.
func (m *Model) gotOSCReply() tea.Cmd {
	if m.oscPending > 0 {
		m.oscPending--
	}
	if m.oscPending == 0 && len(m.deferred) > 0 {
		return m.flushDeferred()
	}
	return nil
}

// whenQuiet runs cmd once no colour answers are in flight.
func (m *Model) whenQuiet(cmd tea.Cmd) tea.Cmd {
	if m.oscPending == 0 || time.Since(m.oscSent) > 2*time.Second {
		m.oscPending = 0
		return cmd
	}
	m.deferred = append(m.deferred, cmd)
	if len(m.deferred) > 1 {
		return nil
	}
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return flushDeferredMsg{} })
}

func (m *Model) flushDeferred() tea.Cmd {
	cmds := m.deferred
	m.deferred = nil
	m.oscPending = 0
	return tea.Batch(cmds...)
}

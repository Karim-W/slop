package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/pkg/tui/termsync"
)

// Live theme sync asks the terminal for its colours. termsync.Gate holds
// quit and terminal hand-offs until every answer is in, so stray replies
// never land in the shell or an editor.

const oscReplies = termsync.Replies

// queryTerm sends the colour queries, unless gitmon is about to quit or
// hand off the terminal.
func (m *Model) queryTerm() tea.Cmd { return m.osc.Query() }

// gotOSCReply counts one answer and releases held commands after the last.
func (m *Model) gotOSCReply() tea.Cmd { return m.osc.Reply() }

// whenQuiet runs cmd once no colour answers are in flight.
func (m *Model) whenQuiet(cmd tea.Cmd) tea.Cmd { return m.osc.WhenQuiet(cmd) }

func (m *Model) flushDeferred() tea.Cmd { return m.osc.Flush() }

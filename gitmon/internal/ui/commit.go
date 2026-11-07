package ui

import (
	"errors"
	"fmt"
	"strings"

	keymap "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/karim-w/gitmon/internal/theme"
)

// Two commit flows, picked by the "commit" setting:
//   - inline (default): a small box, type the message, ⏎ commits
//   - editor: hand the terminal to `git commit`, which opens the editor git
//     is configured with (GIT_EDITOR, core.editor, VISUAL, EDITOR)
// The inline box can always escape to the editor with ctrl+e.

func (m *Model) commitEditorMode() bool { return m.cfg.Commit == "editor" }

func (m *Model) commitForm(amend bool) tea.Cmd {
	if !amend && len(m.status.Staged) == 0 {
		if len(m.status.Unstaged) == 0 {
			return m.toast("Nothing to commit", "working tree clean", toastInfo)
		}
		return m.confirm("Nothing staged", fmt.Sprintf("Stage all %d changes and commit?", len(m.status.Unstaged)), func() tea.Cmd {
			return func() tea.Msg {
				if err := m.repo.StageAll(); err != nil {
					return opMsg{label: "Stage all", err: err}
				}
				return openCommitMsg{}
			}
		})
	}
	if m.commitEditorMode() {
		return m.commitInEditor(amend, "")
	}
	return m.openCommitBox(amend)
}

// commitInEditor runs `git commit` in the foreground. A non-empty draft is
// passed with -m and opened for editing (-e).
func (m *Model) commitInEditor(amend bool, draft string) tea.Cmd {
	args := []string{"commit"}
	if amend {
		args = append(args, "--amend")
	}
	if strings.TrimSpace(draft) != "" {
		args = append(args, "-e", "-m", draft)
	}
	label := "Committed"
	if amend {
		label = "Amended"
	}
	return m.whenQuiet(tea.ExecProcess(m.repo.Interactive(args...), func(err error) tea.Msg {
		if err != nil {
			return opMsg{label: "Commit", err: errors.New("aborted: empty message, editor quit with an error, or a hook failed")}
		}
		return opMsg{label: label}
	}))
}

func (m *Model) toggleCommitMode() tea.Cmd {
	if m.commitEditorMode() {
		m.cfg.Commit = "inline"
	} else {
		m.cfg.Commit = "editor"
	}
	if err := theme.SaveConfig(m.cfg); err != nil {
		return m.toast("Couldn't save setting", err.Error(), toastError)
	}
	if m.commitEditorMode() {
		return m.toast("Commits open in your editor", "c / A now run git commit in $GIT_EDITOR", toastSuccess)
	}
	return m.toast("Commits use the message box", "ctrl+e in the box still opens your editor", toastSuccess)
}

// ─── Inline message box ─────────────────────────────────────────────────────

type commitBox struct {
	ta    textarea.Model
	amend bool
}

func (m *Model) openCommitBox(amend bool) tea.Cmd {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 0
	ta.Placeholder = "what changed — first line is the summary"
	ta.SetStyles(textareaStyles(m.st, false))
	ta.KeyMap.InsertNewline = keymap.NewBinding(keymap.WithKeys("ctrl+j", "alt+enter", "shift+enter"))
	ta.SetHeight(3)
	if amend {
		ta.SetValue(m.repo.LastMessage())
	}
	b := &commitBox{ta: ta, amend: amend}
	return m.open(b, b.ta.Focus())
}

func (b *commitBox) Width(m *Model) int { return min(72, m.w-4) }

func (b *commitBox) View(m *Model) (string, string) {
	s := m.st
	w := b.Width(m) - 6
	b.ta.SetWidth(w)
	b.ta.SetHeight(clamp(b.ta.LineCount(), 3, 10))
	title := fmt.Sprintf("Commit · %s staged", plural(len(m.status.Staged), "file"))
	if b.amend {
		title = "Amend last commit"
	}
	summary, _, _ := strings.Cut(b.ta.Value(), "\n")
	n := len([]rune(summary))
	count := fg(s.Muted).Render(fmt.Sprintf("%d", n))
	if n > 72 {
		count = fg(s.Warning).Render(fmt.Sprintf("%d", n))
	}
	help := fg(s.Accent2).Render("⏎") + fg(s.Muted).Render(" commit  ") +
		fg(s.Accent2).Render("ctrl+j") + fg(s.Muted).Render(" newline  ") +
		fg(s.Accent2).Render("ctrl+e") + fg(s.Muted).Render(" editor  ") +
		fg(s.Accent2).Render("esc") + fg(s.Muted).Render(" cancel")
	foot := truncate(help, w-4)
	foot += strings.Repeat(" ", max(1, w-ansi.StringWidth(foot)-ansi.StringWidth(count))) + count
	return title, b.ta.View() + "\n\n" + foot
}

func (b *commitBox) Update(m *Model, msg tea.Msg) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			return true, nil
		case "enter":
			text := strings.TrimSpace(b.ta.Value())
			if text == "" {
				return false, nil
			}
			label := "Committed"
			if b.amend {
				label = "Amended"
			}
			amend := b.amend
			return true, m.run(label, func() error { return m.repo.Commit(text, amend) })
		case "ctrl+e":
			return true, m.commitInEditor(b.amend, b.ta.Value())
		}
	}
	var cmd tea.Cmd
	b.ta, cmd = b.ta.Update(msg)
	return false, cmd
}

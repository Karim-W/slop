package ui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/karim-w/slop/pkg/tui"
)

// ─── Working tree box ───────────────────────────────────────────────────────

func (m *Model) treePanel(r rect) tui.View {
	focused := m.focus == boxTree
	hints := []tui.Hint{tui.H("space", "stage"), tui.H("a", "all"), tui.H("c", "commit"), tui.H("d", "discard"), tui.H("e", "edit")}
	panel := tui.Panel(nil).Title("Working tree").Focused(focused)
	if m.changeCount() > 0 {
		panel = panel.Label(tui.Line(
			tui.S(fmt.Sprintf("✓%d", len(m.status.Staged)), tui.Fg(tui.Success)),
			tui.S(fmt.Sprintf("/%d staged", len(m.changes())), tui.Fg(tui.Muted)),
		))
	}

	if m.changeCount() == 0 {
		return panel.Hints(hints...).Body(emptyState(
			tui.Gradient("✦ working tree clean ✦").Bold(),
			tui.Text(""),
			tui.Text("nothing to commit").Fg(tui.Muted).Align(tui.Middle),
			tui.If(m.status.Ahead > 0, tui.Textf("↑ %d to push · P", m.status.Ahead).Fg(tui.Success).Align(tui.Middle)),
		))
	}

	if m.flatTree {
		hints = append(hints, tui.H("v", "folders"))
	} else {
		hints = append(hints, tui.H("v", "flat"))
	}
	list := tui.List(len(m.tree), m.ch.sel, &m.ch, func(i int, sel bool) tui.View {
		return m.treeRow(m.tree[i], sel, focused)
	})
	return panel.Hints(hints...).Body(list)
}

// treeRow is one line of the working tree: selection bar, the file or
// folder, and its staged mark, tinted green when fully staged.
func (m *Model) treeRow(row treeRow, sel, focused bool) tui.View {
	var bg tui.Color
	switch {
	case sel && focused && row.state == stageFull:
		bg = tui.Mix(tui.Selection, tui.Success, 0.35)
	case sel && focused:
		bg = tui.Selection
	case row.state == stageFull:
		bg = tui.Mix(tui.Background, tui.Success, 0.2)
	}
	bar := tui.Text("  ")
	if sel {
		bar = tui.Text("▌ ").Fg(tui.Border)
		if focused {
			bar = tui.Text("▌ ").Fg(tui.Accent2)
		}
	}
	mark := tui.Text("  ")
	switch row.state {
	case stageFull:
		mark = tui.Text("✓ ").Fg(tui.Success).Bold()
	case stagePartial:
		mark = tui.Text("◐ ").Fg(tui.Success)
	}
	return tui.Tint(bg, tui.HStack(bar, m.treeRowLine(row).Grow(), mark))
}

func (m *Model) treeRowLine(r treeRow) tui.LineView {
	indent := tui.S(strings.Repeat("  ", r.depth), tui.Style{})
	name := tui.Color(tui.Primary)
	if r.state == stageFull {
		name = tui.Success
	}
	if r.kind == rowFile {
		f := r.file()
		if m.flatTree {
			return tui.Line(append([]tui.Span{indent}, fileSpans(f.Code, f.Path, name)...)...)
		}
		return tui.Line(indent, statusLetter(f.Code), tui.S(" "+r.label, tui.Fg(name)))
	}
	chev := "▾"
	if m.collapsed[r.path] {
		chev = "▸"
	}
	dc := tui.Color(tui.Accent2)
	if r.state == stageFull {
		dc = tui.Success
	}
	return tui.Line(indent,
		tui.S(chev+" ", tui.Fg(tui.Muted)),
		tui.S(r.label+"/", tui.Fg(dc).Bold()),
		tui.S(fmt.Sprintf(" %d", len(r.changes)), tui.Fg(tui.Muted)),
	)
}

// statusColor maps a git status letter to its role.
func statusColor(code byte) tui.Color {
	switch code {
	case 'A', '?':
		return tui.Added
	case 'D':
		return tui.Removed
	case 'R', 'C':
		return tui.Info
	case 'U':
		return tui.Danger
	}
	return tui.Modified
}

// statusLetter is the bold one-letter status of a file (U for untracked).
func statusLetter(code byte) tui.Span {
	letter := string(code)
	if code == '?' {
		letter = "U"
	}
	return tui.S(letter, tui.Fg(statusColor(code)).Bold())
}

// fileSpans renders "M name dir" for a path.
func fileSpans(code byte, path string, name tui.Color) []tui.Span {
	dir, base := filepath.Split(path)
	out := []tui.Span{statusLetter(code), tui.S(" "+base, tui.Fg(name))}
	if dir != "" {
		out = append(out, tui.S(" "+strings.TrimSuffix(dir, "/"), tui.Fg(tui.Muted)))
	}
	return out
}

// ─── Diff / editor box ──────────────────────────────────────────────────────

func (m *Model) diffPanel(r rect) tui.View {
	in := r.inner()
	focused := m.focus == boxDiff
	if m.editing {
		label := tui.Text("saved").Fg(tui.Muted)
		if m.editor.Value() != m.editOrig {
			label = tui.Text("● modified").Fg(tui.Warning).Bold()
		}
		m.editor.SetWidth(in.w)
		m.editor.SetHeight(in.h)
		return tui.Panel(tui.Raw(m.editor.View())).Title("Editor · "+m.editPath).Label(label).
			Focused(focused).Hints(tui.H("ctrl+s", "save"), tui.H("esc", "close"))
	}

	title := "Diff"
	var label []tui.Span
	f, ok := m.selectedChange()
	row, isDir := m.selectedRow()
	isDir = isDir && row.kind == rowDir
	d := m.chDiff.d
	if isDir {
		title = row.path + "/"
		state := map[stageState]string{stageNone: "unstaged", stagePartial: "partly staged", stageFull: "staged"}[row.state]
		label = append(label, tui.S(fmt.Sprintf("%s · %s", state, plural(len(row.changes), "file")), tui.Fg(tui.Muted)))
		if d != nil {
			label = append(label, diffStat(d.Adds, d.Dels)...)
		}
	}
	if ok {
		title = f.Path
		state := "unstaged"
		switch {
		case f.Untracked:
			state = "untracked"
		case f.Staged:
			state = "staged"
		case row.state == stagePartial:
			state = "partly staged · showing unstaged"
		}
		label = []tui.Span{tui.S(state, tui.Fg(tui.Muted))}
		if d != nil {
			label = append(label, diffStat(d.Adds, d.Dels)...)
		}
		if d != nil && len(d.Hunks) > 0 && focused {
			label = append(label, tui.S(fmt.Sprintf(" · hunk %d/%d", m.chDiff.hunk+1, len(d.Hunks)), tui.Fg(tui.Muted)))
		}
	}

	hints := []tui.Hint{tui.H("n/N", "hunk"), tui.H("space", "stage hunk"), tui.H("d", "discard hunk"), tui.H("e", "edit"), tui.H("esc", "back")}
	if isDir {
		verb := "stage folder"
		if row.state == stageFull {
			verb = "unstage folder"
		}
		hints = []tui.Hint{tui.H("space", verb), tui.H("esc", "back")}
	}
	var body tui.View
	if !ok && !isDir {
		body = emptyState(tui.Text("select a file to see its diff").Fg(tui.Muted))
	} else {
		body = m.diffView(&m.chDiff, focused, true, nil)
	}
	panel := tui.Panel(body).Title(title).Focused(focused).Hints(hints...)
	if len(label) > 0 {
		panel = panel.Label(tui.Line(label...))
	}
	return panel
}

// ─── Inline editor ──────────────────────────────────────────────────────────

func (m *Model) styleEditor() { m.editor.SetStyles(textareaStyles(m.st, true)) }

func textareaStyles(s *Styles, cursorLine bool) textarea.Styles {
	st := textarea.DefaultStyles(s.T.Dark)
	for _, ss := range []*textarea.StyleState{&st.Focused, &st.Blurred} {
		ss.Base = lipgloss.NewStyle()
		ss.Text = fg(s.Text)
		ss.LineNumber = fg(s.Muted)
		ss.CursorLineNumber = fg(s.Accent).Bold(true)
		ss.CursorLine = lipgloss.NewStyle()
		if cursorLine {
			ss.CursorLine = lipgloss.NewStyle().Background(s.Sel)
		}
		ss.Placeholder = fg(s.Muted)
		ss.Prompt = fg(s.Border)
		ss.EndOfBuffer = fg(s.Border)
	}
	st.Cursor.Color = s.Accent2
	return st
}

func (m *Model) layoutEditor() {
	_, dr, _ := m.layout()
	in := dr.inner()
	m.editor.SetWidth(max(10, in.w))
	m.editor.SetHeight(max(3, in.h))
}

func (m *Model) openEditor(path string) tea.Cmd {
	full := filepath.Join(m.repo.Root, path)
	b, err := os.ReadFile(full)
	if err != nil {
		return m.toast("Can't edit", err.Error(), toastError)
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return m.toast("Can't edit", "binary file — press E for $EDITOR", toastInfo)
	}
	if len(b) > 1<<20 || bytes.Count(b, []byte("\n")) > 9000 {
		return m.toast("File too large", "press E to open it in $EDITOR", toastInfo)
	}
	ed := textarea.New()
	ed.ShowLineNumbers = true
	ed.CharLimit = 0
	ed.MaxHeight = 0
	ed.MaxWidth = 0
	ed.MaxContentHeight = 20000
	ed.Prompt = ""
	m.editor = ed
	m.styleEditor()
	m.layoutEditor()
	m.editor.SetValue(string(b))
	m.editor.MoveToBegin()
	m.editPath = path
	m.editOrig = m.editor.Value()
	m.editing = true
	m.focus = boxDiff
	return m.editor.Focus()
}

func (m *Model) closeEditor() {
	m.editing = false
	m.editor.Blur()
}

func (m *Model) editorKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "ctrl+s":
		val := m.editor.Value()
		path := m.editPath
		if err := os.WriteFile(filepath.Join(m.repo.Root, path), []byte(val), 0o644); err != nil {
			return m.toast("Save failed", err.Error(), toastError)
		}
		m.editOrig = val
		return tea.Batch(m.toast("Saved "+filepath.Base(path), "", toastSuccess), m.loadAll())
	case "esc":
		if m.editor.Value() != m.editOrig {
			return m.confirm("Unsaved edits", "Close the editor and throw away your edits to "+m.editPath+"?", func() tea.Cmd {
				m.closeEditor()
				return nil
			})
		}
		m.closeEditor()
		return nil
	case "ctrl+c":
		return m.whenQuiet(tea.Quit)
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(k)
	return cmd
}

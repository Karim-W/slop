package ui

import (
	"bytes"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/karim-w/gitmon/internal/git"
)

// ─── Working tree box ───────────────────────────────────────────────────────

func (m *Model) viewTree(r rect) string {
	s := m.rs
	in := r.inner()
	focused := m.focus == boxTree
	right := ""
	if n := m.changeCount(); n > 0 {
		right = fg(s.Success).Render(fmt.Sprintf("✓%d", len(m.status.Staged))) + fg(s.Muted).Render(fmt.Sprintf("/%d staged", len(m.changes())))
	}
	hints := []hint{{"space", "stage"}, {"a", "all"}, {"c", "commit"}, {"d", "discard"}, {"e", "edit"}}

	if m.changeCount() == 0 {
		body := centered([]string{
			gradient("✦ working tree clean ✦", true, s.Accent, s.Accent2, s.Accent3),
			"",
			fg(s.Muted).Render("nothing to commit"),
		}, in.w, in.h)
		if m.status.Ahead > 0 {
			body += "\n" + lipgloss.PlaceHorizontal(in.w, lipgloss.Center, fg(s.Success).Render(fmt.Sprintf("↑ %d to push · P", m.status.Ahead)))
		}
		return m.frame(r, focused, "Working tree", right, hints, body)
	}

	L := make([]string, len(m.tree))
	stagedBg := theme_mix(s.Bg, s.Success, 0.2)
	for li, row := range m.tree {
		sel := li == m.ch.sel
		var bg color.Color
		switch {
		case sel && focused && row.state == stageFull:
			bg = theme_mix(s.Sel, s.Success, 0.35)
		case sel && focused:
			bg = s.Sel
		case row.state == stageFull:
			bg = stagedBg
		}
		bar := on(s.Text, bg).Render("  ")
		if sel {
			bc := s.Border
			if focused {
				bc = s.Accent2
			}
			bar = on(bc, bg).Render("▌ ")
		}
		mark := on(s.Text, bg).Render("  ")
		switch row.state {
		case stageFull:
			mark = on(s.Success, bg).Bold(true).Render("✓ ")
		case stagePartial:
			mark = on(s.Success, bg).Render("◐ ")
		}
		L[li] = padBg(bar+m.treeRowLine(row, in.w-5, bg), in.w-2, bg) + mark
	}
	if m.flatTree {
		hints = append(hints, hint{"v", "folders"})
	} else {
		hints = append(hints, hint{"v", "flat"})
	}
	off := m.ch.offset(in.h, len(L))
	end := min(len(L), off+in.h)
	return m.frame(r, focused, "Working tree", right, hints, strings.Join(L[off:end], "\n"))
}

func (m *Model) treeRowLine(r treeRow, w int, bg color.Color) string {
	s := m.rs
	indent := on(s.Text, bg).Render(strings.Repeat("  ", r.depth))
	name := s.Text
	if r.state == stageFull {
		name = s.Success
	}
	if r.kind == rowFile {
		f := r.file()
		if m.flatTree {
			return indent + m.fileLine(f, w-2*r.depth, bg, name)
		}
		letter := string(f.Code)
		if f.Code == '?' {
			letter = "U"
		}
		out := on(m.statusColor(f.Code), bg).Bold(true).Render(letter) + on(name, bg).Render(" "+r.label)
		return truncate(indent+out, w)
	}
	chev := "▾"
	if m.collapsed[r.path] {
		chev = "▸"
	}
	dc := s.Accent2
	if r.state == stageFull {
		dc = s.Success
	}
	out := indent + on(s.Muted, bg).Render(chev+" ") + on(dc, bg).Bold(true).Render(r.label+"/") +
		on(s.Muted, bg).Render(fmt.Sprintf(" %d", len(r.changes)))
	return truncate(out, w)
}

func (m *Model) statusColor(code byte) color.Color {
	s := m.rs
	switch code {
	case 'A', '?':
		return s.Added
	case 'D':
		return s.Removed
	case 'R', 'C':
		return s.Info
	case 'U':
		return s.Error
	}
	return s.Modified
}

func (m *Model) fileLine(f git.FileStatus, w int, bg, name color.Color) string {
	s := m.rs
	letter := string(f.Code)
	if f.Code == '?' {
		letter = "U"
	}
	dir, base := filepath.Split(f.Path)
	out := on(m.statusColor(f.Code), bg).Bold(true).Render(letter) + on(name, bg).Render(" "+base)
	if dir != "" {
		out += on(s.Muted, bg).Render(" " + strings.TrimSuffix(dir, "/"))
	}
	return truncate(out, w)
}

// ─── Diff / editor box ──────────────────────────────────────────────────────

func (m *Model) viewDiffBox(r rect) string {
	s := m.rs
	in := r.inner()
	focused := m.focus == boxDiff
	if m.editing {
		title := "Editor · " + m.editPath
		right := fg(s.Muted).Render("saved")
		if m.editor.Value() != m.editOrig {
			right = fg(s.Warning).Bold(true).Render("● modified")
		}
		m.editor.SetWidth(in.w)
		m.editor.SetHeight(in.h)
		return m.frame(r, focused, title, right, []hint{{"ctrl+s", "save"}, {"esc", "close"}}, m.editor.View())
	}
	title := "Diff"
	right := ""
	f, ok := m.selectedChange()
	row, isDir := m.selectedRow()
	isDir = isDir && row.kind == rowDir
	if isDir {
		title = row.path + "/"
		state := map[stageState]string{stageNone: "unstaged", stagePartial: "partly staged", stageFull: "staged"}[row.state]
		right = fg(s.Muted).Render(fmt.Sprintf("%s · %s", state, plural(len(row.changes), "file")))
		if d := m.chDiff.d; d != nil && d.Adds+d.Dels > 0 {
			right += " " + fg(s.Added).Render(fmt.Sprintf("+%d", d.Adds)) + " " + fg(s.Removed).Render(fmt.Sprintf("−%d", d.Dels))
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
		right = fg(s.Muted).Render(state)
		if d := m.chDiff.d; d != nil && d.Adds+d.Dels > 0 {
			right += " " + fg(s.Added).Render(fmt.Sprintf("+%d", d.Adds)) + " " + fg(s.Removed).Render(fmt.Sprintf("−%d", d.Dels))
		}
		if d := m.chDiff.d; d != nil && len(d.Hunks) > 0 && focused {
			right += fg(s.Muted).Render(fmt.Sprintf(" · hunk %d/%d", m.chDiff.hunk+1, len(d.Hunks)))
		}
	}
	hints := []hint{{"n/N", "hunk"}, {"space", "stage hunk"}, {"d", "discard hunk"}, {"e", "edit"}, {"esc", "back"}}
	var body string
	if isDir {
		hints = []hint{{"space", "stage folder"}, {"esc", "back"}}
		if row.state == stageFull {
			hints[0].d = "unstage folder"
		}
	}
	if !ok && !isDir {
		body = centered([]string{fg(s.Muted).Render("select a file to see its diff")}, in.w, in.h)
	} else {
		body = m.renderDiff(&m.chDiff, in.w, in.h, focused, true, "")
	}
	return m.frame(r, focused, title, right, hints, body)
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

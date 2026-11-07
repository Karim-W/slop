package ui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/gitmon/internal/git"
	"github.com/karim-w/slop/pkg/domain/theme"
	"github.com/karim-w/slop/pkg/tui"
)

// diffPane is a scrollable, hunk-aware diff viewer.
type diffPane struct {
	key     string
	d       *git.Diff
	err     error
	loading bool
	off     spring
	hunk    int
	vis     []int // indices into d.Lines that are displayed
	fileAt  map[int]string
}

func newDiffPane() diffPane { return diffPane{off: newSpring(&scrollSpring, 0)} }

func (p *diffPane) clear() {
	p.key, p.d, p.err, p.loading, p.vis = "", nil, nil, false, nil
	p.off.Snap(0)
	p.hunk = 0
}

// load fetches a diff asynchronously. keep preserves scroll position (used for
// live refreshes of the same file).
func (p *diffPane) load(key string, keep bool, fn func() (string, error)) tea.Cmd {
	p.key = key
	p.loading = true
	if !keep {
		p.off.Snap(0)
		p.hunk = 0
	}
	return func() tea.Msg {
		out, err := fn()
		return diffMsg{pane: p, key: key, d: git.ParseDiff(out), err: err}
	}
}

func (p *diffPane) setDiff(d *git.Diff, err error) {
	p.d, p.err, p.loading = d, err, false
	p.vis = p.vis[:0]
	p.fileAt = map[int]string{}
	if d == nil {
		return
	}
	for i, l := range d.Lines {
		if l.Kind == git.LineMeta {
			t := l.Text
			switch {
			case strings.HasPrefix(t, "diff --git"):
				// "diff --git a/x b/x" -> show as a file bar
				parts := strings.SplitN(t, " b/", 2)
				name := t
				if len(parts) == 2 {
					name = parts[1]
				}
				p.fileAt[i] = name
			case strings.HasPrefix(t, "index "), strings.HasPrefix(t, "--- "), strings.HasPrefix(t, "+++ "):
				continue
			}
		}
		p.vis = append(p.vis, i)
	}
	if len(p.fileAt) == 1 { // single file: the pane title already names it
		vis := p.vis[:0]
		for _, v := range p.vis {
			if _, ok := p.fileAt[v]; !ok {
				vis = append(vis, v)
			}
		}
		p.vis = vis
	}
	if len(d.Hunks) > 0 {
		p.hunk = clamp(p.hunk, 0, len(d.Hunks)-1)
	} else {
		p.hunk = 0
	}
	p.off.Target = float64(clamp(int(p.off.Target), 0, max(0, len(p.vis)-1)))
}

func (p *diffPane) scroll(delta, height int) {
	maxOff := max(0, len(p.vis)-height+2)
	p.off.Set(float64(clamp(int(p.off.Target)+delta, 0, maxOff)))
}

func (p *diffPane) visIndexOf(line int) int {
	for i, v := range p.vis {
		if v >= line {
			return i
		}
	}
	return 0
}

func (p *diffPane) jumpHunk(delta, height int) {
	if p.d == nil || len(p.d.Hunks) == 0 {
		return
	}
	p.hunk = clamp(p.hunk+delta, 0, len(p.d.Hunks)-1)
	vi := p.visIndexOf(p.d.Hunks[p.hunk].Start)
	maxOff := max(0, len(p.vis)-height+2)
	p.off.Set(float64(clamp(vi-2, 0, maxOff)))
}

// syncHunkToScroll selects the hunk nearest the top of the viewport.
func (p *diffPane) syncHunkToScroll() {
	if p.d == nil || len(p.d.Hunks) == 0 {
		return
	}
	top := int(p.off.Target) + 2
	if top >= len(p.vis) {
		return
	}
	line := p.vis[clamp(top, 0, len(p.vis)-1)]
	for i, h := range p.d.Hunks {
		if line >= h.Start && line < h.End {
			p.hunk = i
			return
		}
		if h.Start > line {
			p.hunk = max(0, i-1)
			return
		}
	}
}

// diffView shows a diff pane as a view that fills the space it gets. With a
// title, the first line names the file and its stats.
func (m *Model) diffView(p *diffPane, focused, hunkMode bool, title []tui.Span) tui.View {
	return tui.Func(func(c tui.Ctx, size tui.Size) []string {
		return diffLines(c.P, p, size.W, size.H, focused, hunkMode, title)
	})
}

// diffLines draws the diff into w×h with palette s: hunk headers, gutters
// with line numbers, and tinted added and removed lines.
func diffLines(s *Styles, p *diffPane, w, h int, focused, hunkMode bool, title []tui.Span) []string {
	lines := make([]string, 0, h)

	// title bar
	if len(title) > 0 {
		if p.d != nil {
			title = append(title, diffStat(p.d.Adds, p.d.Dels)...)
		}
		if hunkMode && focused && p.d != nil && len(p.d.Hunks) > 0 {
			title = append(title, tui.S(fmt.Sprintf("  hunk %d/%d", p.hunk+1, len(p.d.Hunks)), tui.Fg(tui.Muted)))
		}
		lines = append(lines, pad(tui.Spans(s, title...), w))
		h--
	}

	switch {
	case p.err != nil:
		lines = append(lines, fg(s.Error).Render(truncate(" "+p.err.Error(), w)))
		return fillLines(lines, w, h+1)
	case p.d == nil && p.loading:
		lines = append(lines, fg(s.Muted).Render(" loading…"))
		return fillLines(lines, w, h+1)
	case p.d == nil || len(p.vis) == 0:
		lines = append(lines, "", fg(s.Muted).Render(" nothing to show"))
		return fillLines(lines, w, h+1)
	case p.d.Binary && len(p.d.Hunks) == 0:
		lines = append(lines, "", fg(s.Muted).Render(" binary file"))
		return fillLines(lines, w, h+1)
	}

	off := clamp(p.off.Int(), 0, max(0, len(p.vis)-1))
	gutter := 10
	textW := max(1, w-gutter-1)
	for row := 0; row < h; row++ {
		vi := off + row
		if vi >= len(p.vis) {
			lines = append(lines, "")
			continue
		}
		li := p.vis[vi]
		l := p.d.Lines[li]
		selHunk := hunkMode && focused && l.Hunk == p.hunk && l.Hunk >= 0
		bar := " "
		if selHunk {
			bar = fg(s.Accent2).Render("▌")
		}
		var bg color.Color
		var gut, text string
		switch l.Kind {
		case git.LineMeta:
			if name, ok := p.fileAt[li]; ok {
				dir, base := filepath.Split(name)
				label := fg(s.Muted).Render(" "+dir) + fg(s.Text).Bold(true).Render(base) + " "
				lines = append(lines, on(s.Text, s.Surface).Render(padBg(" ◇"+label, w, s.Surface)))
				continue
			}
			lines = append(lines, fg(s.Muted).Italic(true).Render(truncate("  "+l.Text, w)))
			continue
		case git.LineHunk:
			hb := s.HunkBg
			if selHunk {
				hb = theme.Mix(s.Bg, s.Accent2, 0.22)
			}
			ctx := ""
			if i := strings.LastIndex(l.Text, "@@"); i > 1 {
				ctx = strings.TrimSpace(l.Text[i+2:])
				l.Text = l.Text[:i+2]
			}
			txt := fg(s.Accent).Background(hb).Render(" "+l.Text+" ") + fg(s.Sub).Background(hb).Render(ctx)
			lines = append(lines, bar+padBg(txt, w-1, hb))
			continue
		case git.LineAdd:
			bg = s.AddBg
			gut = fmt.Sprintf("%4s %4d", "", l.New)
			text = fg(s.Added).Background(bg).Bold(true).Render("+") + on(s.Text, bg).Render(expand(l.Text))
		case git.LineDel:
			bg = s.DelBg
			gut = fmt.Sprintf("%4d %4s", l.Old, "")
			text = fg(s.Removed).Background(bg).Bold(true).Render("-") + on(s.Text, bg).Render(expand(l.Text))
		case git.LineNoNewline:
			gut = strings.Repeat(" ", 9)
			text = fg(s.Muted).Italic(true).Render(l.Text)
		default:
			gut = fmt.Sprintf("%4d %4d", l.Old, l.New)
			text = on(s.Sub, nil).Render(" " + expand(l.Text))
		}
		gs := on(s.Muted, bg).Render(gut + " ")
		if l.Kind == git.LineAdd {
			gs = on(s.Added, bg).Render(gut + " ")
		} else if l.Kind == git.LineDel {
			gs = on(s.Removed, bg).Render(gut + " ")
		}
		lines = append(lines, bar+gs+padBg(text, textW, bg))
	}
	return lines
}

func expand(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

func fillLines(lines []string, _, h int) []string {
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

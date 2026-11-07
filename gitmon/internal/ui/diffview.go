package ui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/karim-w/gitmon/internal/git"
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
	p.off.snap(0)
	p.hunk = 0
}

// load fetches a diff asynchronously. keep preserves scroll position (used for
// live refreshes of the same file).
func (p *diffPane) load(key string, keep bool, fn func() (string, error)) tea.Cmd {
	p.key = key
	p.loading = true
	if !keep {
		p.off.snap(0)
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
	p.off.target = float64(clamp(int(p.off.target), 0, max(0, len(p.vis)-1)))
}

func (p *diffPane) scroll(delta, height int) {
	maxOff := max(0, len(p.vis)-height+2)
	p.off.set(float64(clamp(int(p.off.target)+delta, 0, maxOff)))
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
	p.off.set(float64(clamp(vi-2, 0, maxOff)))
}

// syncHunkToScroll selects the hunk nearest the top of the viewport.
func (p *diffPane) syncHunkToScroll() {
	if p.d == nil || len(p.d.Hunks) == 0 {
		return
	}
	top := int(p.off.target) + 2
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

// render draws the diff into w×h.
func (m *Model) renderDiff(p *diffPane, w, h int, focused, hunkMode bool, title string) string {
	s := m.rs
	lines := make([]string, 0, h)

	// title bar
	head := title
	if p.d != nil && (p.d.Adds+p.d.Dels) > 0 {
		head += "  " + fg(s.Added).Render(fmt.Sprintf("+%d", p.d.Adds)) + " " + fg(s.Removed).Render(fmt.Sprintf("−%d", p.d.Dels))
	}
	if hunkMode && focused && p.d != nil && len(p.d.Hunks) > 0 {
		head += fg(s.Muted).Render(fmt.Sprintf("  hunk %d/%d", p.hunk+1, len(p.d.Hunks)))
	}
	if title != "" {
		lines = append(lines, pad(head, w))
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

	off := clamp(p.off.int(), 0, max(0, len(p.vis)-1))
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
				hb = theme_mix(s.Bg, s.Accent2, 0.22)
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
	return strings.Join(lines, "\n")
}

func theme_mix(a, b color.Color, t float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	l := func(x, y uint32) uint8 { return uint8(float64(x>>8)*(1-t) + float64(y>>8)*t) }
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", l(ar, br), l(ag, bg), l(ab, bb)))
}

func expand(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

func fillLines(lines []string, w, h int) string {
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

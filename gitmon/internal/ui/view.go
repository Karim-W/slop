package ui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ─── Geometry ───────────────────────────────────────────────────────────────

type rect struct{ x, y, w, h int }

func (r rect) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// inner is the content area inside the border.
func (r rect) inner() rect { return rect{r.x + 1, r.y + 1, max(0, r.w-2), max(0, r.h-2)} }

func (m *Model) layout() (tree, diff, hist rect) {
	mid := max(6, m.h-2) // minus header + footer
	gh := clamp(mid*46/100, 8, max(8, mid-8))
	th := mid - gh
	lw := clamp(m.w*36/100, 30, 64)
	if m.w < 80 {
		lw = m.w / 2
	}
	tree = rect{0, 1, lw, th}
	diff = rect{lw, 1, m.w - lw, th}
	hist = rect{0, 1 + th, m.w, gh}
	return
}

// ─── Root view ──────────────────────────────────────────────────────────────

func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
	v.WindowTitle = "gitmon · " + m.repo.Name
	if !m.termMode {
		// Paint the terminal with the theme. In terminal (Rex) mode we leave
		// the host colours alone so they can be mirrored live.
		v.BackgroundColor = m.st.Bg
		v.ForegroundColor = m.st.Text
	}
	if !m.ready || m.awaitTheme {
		v.SetContent("")
		return v
	}
	if m.w < 50 || m.h < 16 {
		v.SetContent(fg(m.st.Muted).Render("gitmon needs at least 50×16"))
		return v
	}

	m.rs = m.st
	if m.modal != nil {
		m.rs = m.dim
	}
	tr, dr, hr := m.layout()
	top := hcat(tr.h,
		col{m.viewTree(tr), tr.w},
		col{m.viewDiffBox(dr), dr.w},
	)
	base := strings.Join([]string{m.viewHeader(), top, m.viewHistory(hr), m.viewFooter()}, "\n")

	layers := []*lipgloss.Layer{lipgloss.NewLayer(base)}
	if m.modal == nil && m.focus == boxGraph && !m.searching {
		layers = append(layers, m.tooltipLayers(hr)...)
	}
	m.rs = m.st
	if m.modal != nil {
		box := m.renderModal()
		bw, bh := lipgloss.Width(box), lipgloss.Height(box)
		x := max(0, (m.w-bw)/2)
		y := max(1, (m.h-bh)/2) + m.modalY.int()
		layers = append(layers, lipgloss.NewLayer(box).X(x).Y(max(0, y)).Z(5))
	}
	ty := m.h - 1
	for i := len(m.toasts) - 1; i >= 0; i-- {
		t := m.toasts[i]
		box := m.renderToast(t)
		ty -= lipgloss.Height(box)
		if ty < 1 {
			break
		}
		x := m.w - toastWidth - 2 + t.x.int()
		if x < m.w-1 {
			layers = append(layers, lipgloss.NewLayer(clipRight(box, m.w-x)).X(x).Y(ty).Z(8))
		}
	}
	if len(layers) == 1 {
		v.SetContent(base)
	} else {
		v.SetContent(lipgloss.NewCompositor(layers...).Render())
	}
	return v
}

// ─── Box chrome ─────────────────────────────────────────────────────────────

// frame draws a rounded box with a title and right label in the top border
// and key hints in the bottom border.
func (m *Model) frame(r rect, focused bool, title, right string, hints []hint, body string) string {
	s := m.rs
	w, h := r.w, r.h
	if w < 4 || h < 2 {
		return ""
	}
	var edge []color.Color
	if focused {
		edge = lipgloss.Blend1D(w, s.Accent, s.Accent2, s.Accent3)
	} else {
		edge = make([]color.Color, w)
		for i := range edge {
			edge[i] = s.Border
		}
	}
	line := func(left, fill, rightc string, label, rlabel string) string {
		var b strings.Builder
		b.WriteString(fg(edge[0]).Render(left))
		used := 1
		if label != "" {
			b.WriteString(fg(edge[1]).Render("─"))
			b.WriteString(label)
			used += 1 + ansi.StringWidth(label)
		}
		rw := ansi.StringWidth(rlabel)
		for x := used; x < w-1-rw-1 && x < w-1; x++ {
			b.WriteString(fg(edge[x]).Render(fill))
			used++
		}
		if rlabel != "" && used+rw+1 < w {
			b.WriteString(rlabel)
			used += rw
			b.WriteString(fg(edge[min(used, w-2)]).Render(fill))
			used++
		}
		for ; used < w-1; used++ {
			b.WriteString(fg(edge[used]).Render(fill))
		}
		b.WriteString(fg(edge[w-1]).Render(rightc))
		return b.String()
	}

	tc := s.Sub
	if focused {
		tc = s.Text
	}
	tl := ""
	if title != "" {
		tl = fg(tc).Bold(true).Render(" " + title + " ")
		if focused {
			tl = fg(s.Accent).Bold(true).Render(" ● ") + fg(tc).Bold(true).Render(title+" ")
		}
	}
	rl := ""
	if right != "" {
		rl = " " + right + " "
	}
	bl := ""
	if focused && len(hints) > 0 {
		var parts []string
		for _, hn := range hints {
			parts = append(parts, fg(s.Accent).Bold(true).Render(hn.k)+" "+fg(s.Muted).Render(hn.d))
		}
		for n := len(parts); n > 0; n-- {
			bl = " " + strings.Join(parts[:n], fg(s.Border).Render(" · ")) + " "
			if ansi.StringWidth(bl) < w-4 {
				break
			}
			bl = ""
		}
	}

	out := make([]string, 0, h)
	out = append(out, line("╭", "─", "╮", truncate(tl, w-4), truncate(rl, max(0, w-6-ansi.StringWidth(tl)))))
	lines := strings.Split(body, "\n")
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, fg(edge[0]).Render("│")+pad(l, w-2)+fg(edge[w-1]).Render("│"))
	}
	out = append(out, line("╰", "─", "╯", bl, ""))
	return strings.Join(out, "\n")
}

// ─── Header & footer ────────────────────────────────────────────────────────

func (m *Model) viewHeader() string {
	s := m.rs
	logo := gradient(" ◆ gitmon ", true, s.Accent, s.Accent2, s.Accent3)
	left := logo + fg(s.Border).Render("│ ") + fg(s.Text).Bold(true).Render(m.repo.Name) + " "
	st := m.status
	branch := st.Branch
	if st.Detached {
		branch = "detached @ " + short(st.Head)
	}
	if branch == "" {
		branch = "…"
	}
	left += chip("⎇ "+branch, s.Accent, theme_mix(s.Bg, s.Accent, 0.18), true)
	if st.Ahead > 0 {
		left += " " + fg(s.Success).Render(fmt.Sprintf("↑%d", st.Ahead))
	}
	if st.Behind > 0 {
		left += " " + fg(s.Warning).Render(fmt.Sprintf("↓%d", st.Behind))
	}
	if st.Ahead == 0 && st.Behind == 0 && st.Upstream != "" {
		left += " " + fg(s.Muted).Render("✓ synced")
	}
	if st.State != "" {
		left += " " + chip(strings.ToUpper(st.State), s.Bg, s.Warning, true)
	}
	if n := len(m.stashes); n > 0 {
		left += " " + fg(s.Muted).Render(fmt.Sprintf("◇ %d stashed", n))
	}

	var right string
	if m.busy > 0 {
		right = m.spin.View() + " " + fg(s.Sub).Render(m.busyLabel+"…") + "  "
	}
	themeLabel := "◐ " + s.T.Name
	if m.termMode {
		themeLabel = "◐ " + s.T.Name + " (live)"
	}
	right += fg(s.Muted).Render(themeLabel) + " "
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncate(left, m.w)
	}
	return left + strings.Repeat(" ", gap) + right
}

type hint struct{ k, d string }

func (m *Model) viewFooter() string {
	s := m.rs
	hs := []hint{{"tab", "switch box"}, {"c", "commit"}, {"f", "fetch"}, {"p", "pull"}, {"P", "push"}, {"B", "branches"}, {"Z", "stashes"}, {"T", "theme"}, {"ctrl+p", "commands"}, {"q", "quit"}}
	if m.searching {
		hs = []hint{{"enter", "keep"}, {"esc", "clear"}, {"↑↓", "prev/next match"}}
	}
	if m.editing && m.focus == boxDiff {
		hs = []hint{{"ctrl+s", "save"}, {"esc", "close editor"}}
	}
	var parts []string
	for _, h := range hs {
		parts = append(parts, fg(s.Accent2).Bold(true).Render(h.k)+" "+fg(s.Muted).Render(h.d))
	}
	out := ""
	for n := len(parts); n >= 0; n-- {
		out = " " + strings.Join(parts[:n], fg(s.Border).Render("  "))
		if ansi.StringWidth(out) <= m.w {
			break
		}
	}
	return out
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func fitBlock(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = pad(l, w)
	}
	return strings.Join(lines, "\n")
}

func clipRight(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > w {
			lines[i] = ansi.Truncate(l, w, "")
		}
	}
	return strings.Join(lines, "\n")
}

type col = struct {
	s string
	w int
}

func hcat(h int, parts ...col) string {
	cols := make([][]string, len(parts))
	for i, p := range parts {
		cols[i] = strings.Split(fitBlock(p.s, p.w, h), "\n")
	}
	out := make([]string, h)
	for r := 0; r < h; r++ {
		var b strings.Builder
		for i := range parts {
			b.WriteString(cols[i][r])
		}
		out[r] = b.String()
	}
	return strings.Join(out, "\n")
}

func short(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

func (m *Model) section(title string, count int, c lipgloss.Style) string {
	t := " " + c.Bold(true).Render(strings.ToUpper(title))
	if count >= 0 {
		t += " " + fg(m.rs.Muted).Render(fmt.Sprint(count))
	}
	return t
}

func wrap(text string, w int) []string {
	if w <= 0 {
		return nil
	}
	out := lipgloss.NewStyle().Width(w).Render(text)
	lines := strings.Split(out, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}

func centered(lines []string, w, h int) string {
	var out []string
	top := max(0, (h-len(lines))/2)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, l := range lines {
		out = append(out, lipgloss.PlaceHorizontal(w, lipgloss.Center, l))
	}
	return strings.Join(out, "\n")
}

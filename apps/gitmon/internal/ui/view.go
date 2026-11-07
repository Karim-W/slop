package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/pkg/tui"
)

// ─── Geometry ───────────────────────────────────────────────────────────────

// rect is a box on screen. The layout is computed once here and used both to
// size the views and to hit-test the mouse, so the two always agree.
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
	v.SetContent(tui.Render(m.st, m.root(), m.w, m.h))
	return v
}

// root is the whole screen:
//
//	header
//	working tree │ diff
//	history
//	footer
//
// with the commit tooltip, a modal and toasts floating above.
func (m *Model) root() tui.View {
	if m.w < 50 || m.h < 16 {
		return tui.Text("gitmon needs at least 50×16").Fg(tui.Muted)
	}
	tr, dr, hr := m.layout()
	base := tui.VStack(
		m.header(),
		tui.Height(tr.h, tui.HStack(
			tui.Width(tr.w, m.treePanel(tr)),
			tui.Width(dr.w, m.diffPanel(dr)),
		)),
		tui.Height(hr.h, m.historyPanel(hr)),
		m.footer(),
	)

	var layers []tui.Layer
	if m.modal == nil && m.focus == boxGraph && !m.searching {
		if tip, ok := m.tooltip(hr); ok {
			layers = append(layers, tip)
		}
	}
	if m.modal != nil {
		layers = append(layers, tui.Float(m.modalView()).Offset(0, m.modalY.Int()))
	}
	layers = append(layers, m.toastLayers()...)

	z := tui.ZStack(base, layers...)
	if m.modal != nil {
		z = z.Scrim(0.62)
	}
	return z
}

// ─── Header & footer ────────────────────────────────────────────────────────

func (m *Model) header() tui.View {
	st := m.status
	branch := st.Branch
	if st.Detached {
		branch = "detached @ " + short(st.Head)
	}
	if branch == "" {
		branch = "…"
	}
	left := []tui.View{
		tui.Gradient(" ◆ gitmon ").Bold(),
		tui.Text("│ ").Fg(tui.Border),
		tui.Text(m.repo.Name + " ").Bold(),
		tui.Chip("⎇ "+branch, tui.Accent, tui.Mix(tui.Background, tui.Accent, 0.18)).Bold(),
	}
	if st.Ahead > 0 {
		left = append(left, tui.Textf(" ↑%d", st.Ahead).Fg(tui.Success))
	}
	if st.Behind > 0 {
		left = append(left, tui.Textf(" ↓%d", st.Behind).Fg(tui.Warning))
	}
	if st.Ahead == 0 && st.Behind == 0 && st.Upstream != "" {
		left = append(left, tui.Text(" ✓ synced").Fg(tui.Muted))
	}
	if st.State != "" {
		left = append(left, tui.Text(" "), tui.Chip(strings.ToUpper(st.State), tui.Background, tui.Warning).Bold())
	}
	if n := len(m.stashes); n > 0 {
		left = append(left, tui.Textf(" ◇ %d stashed", n).Fg(tui.Muted))
	}

	themeLabel := "◐ " + m.st.T.Name
	if m.termMode {
		themeLabel += " (live)"
	}
	return tui.HStack(
		tui.Group(left...),
		tui.Spacer(),
		tui.If(m.busy > 0, tui.HStack(tui.Raw(m.spin.View()), tui.Text(" "+m.busyLabel+"…  ").Fg(tui.Secondary))),
		tui.Text(themeLabel+" ").Fg(tui.Muted),
	)
}

func (m *Model) footer() tui.View {
	hs := []tui.Hint{tui.H("tab", "switch box"), tui.H("c", "commit"), tui.H("f", "fetch"), tui.H("p", "pull"),
		tui.H("P", "push"), tui.H("B", "branches"), tui.H("Z", "stashes"), tui.H("T", "theme"),
		tui.H("ctrl+p", "commands"), tui.H("q", "quit")}
	if m.searching {
		hs = []tui.Hint{tui.H("enter", "keep"), tui.H("esc", "clear"), tui.H("↑↓", "prev/next match")}
	}
	if m.editing && m.focus == boxDiff {
		hs = []tui.Hint{tui.H("ctrl+s", "save"), tui.H("esc", "close editor")}
	}
	return tui.HStack(tui.Text(" "), tui.KeyHints(hs...).Sep("  "))
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// emptyState is a centred message for a box with nothing to show.
func emptyState(lines ...tui.View) tui.View {
	return tui.Center(tui.VStack(lines...))
}

// diffStat is "+12 −3" in the diff colours, or nothing for an empty diff.
func diffStat(adds, dels int) []tui.Span {
	if adds+dels == 0 {
		return nil
	}
	return []tui.Span{
		tui.S(fmt.Sprintf(" +%d", adds), tui.Fg(tui.Added)),
		tui.S(fmt.Sprintf(" −%d", dels), tui.Fg(tui.Removed)),
	}
}

func short(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

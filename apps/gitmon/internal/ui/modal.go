package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/karim-w/slop/apps/gitmon/internal/config"
	"github.com/karim-w/slop/pkg/domain/theme"
	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/tui/termsync"
	"github.com/karim-w/slop/pkg/utils/textx"
)

// modal is a floating dialog. View returns its title and body; Width is the
// card's outer width, border included.
type modal interface {
	Update(m *Model, msg tea.Msg) (closed bool, cmd tea.Cmd)
	View(m *Model) (title string, body tui.View)
	Width(m *Model) int
}

func (m *Model) open(md modal, init tea.Cmd) tea.Cmd {
	m.modal = md
	m.modalY.Snap(-4)
	m.modalY.Set(0)
	return tea.Batch(init, m.kick())
}

// modalView wraps the open modal in the design language's dialog card.
func (m *Model) modalView() tui.View {
	title, body := m.modal.View(m)
	return tui.Dialog(title, body).Width(m.modal.Width(m))
}

// ─── huh form modal ─────────────────────────────────────────────────────────

type formModal struct {
	title    string
	form     *huh.Form
	width    int
	onSubmit func() tea.Cmd
}

func (m *Model) openForm(title string, width int, onSubmit func() tea.Cmd, groups ...*huh.Group) tea.Cmd {
	f := huh.NewForm(groups...).
		WithTheme(huhTheme(m.st)).
		WithWidth(width - 6).
		WithShowHelp(true).
		WithShowErrors(true)
	fm := &formModal{title: title, form: f, width: width, onSubmit: onSubmit}
	return m.open(fm, f.Init())
}

func (f *formModal) Width(m *Model) int { return min(f.width, m.w-4) }

func (f *formModal) View(m *Model) (string, tui.View) {
	return f.title, tui.Raw(strings.TrimRight(f.form.View(), "\n "))
}

func (f *formModal) Update(m *Model, msg tea.Msg) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "esc" {
		return true, nil
	}
	nf, cmd := f.form.Update(msg)
	if ff, ok := nf.(*huh.Form); ok {
		f.form = ff
	}
	switch f.form.State {
	case huh.StateCompleted:
		return true, tea.Batch(cmd, f.onSubmit())
	case huh.StateAborted:
		return true, cmd
	}
	return false, cmd
}

// confirm asks a yes/no question and runs fn on yes.
func (m *Model) confirm(title, desc string, fn func() tea.Cmd) tea.Cmd {
	ok := false
	return m.openForm(title, 56, func() tea.Cmd {
		if ok {
			return fn()
		}
		return nil
	}, huh.NewGroup(huh.NewConfirm().Title(desc).Affirmative("Do it").Negative("Cancel").Value(&ok)))
}

// prompt asks for a single line of text.
func (m *Model) prompt(title, label, placeholder, initial string, fn func(string) tea.Cmd) tea.Cmd {
	val := initial
	return m.openForm(title, 60, func() tea.Cmd {
		v := strings.TrimSpace(val)
		if v == "" {
			return nil
		}
		return fn(v)
	}, huh.NewGroup(huh.NewInput().Title(label).Placeholder(placeholder).Value(&val).Validate(func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("required")
		}
		if strings.ContainsAny(s, " ~^:?*[\\") && strings.Contains(strings.ToLower(title), "branch") {
			return fmt.Errorf("invalid ref name")
		}
		return nil
	})))
}

// ─── Command palette ────────────────────────────────────────────────────────

type command struct {
	title string
	key   string
	group string
	run   func(m *Model) tea.Cmd
}

type paletteModal struct {
	title   string
	input   textinput.Model
	all     []command
	matches []int
	sel     int
	sc      scroller
}

const paletteRows = 14

func (m *Model) openPalette(title string) tea.Cmd {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "type a command, branch or theme…"
	ss := textinput.DefaultStyles(m.st.T.Dark)
	ss.Focused.Prompt = fg(m.st.Accent2).Bold(true)
	ss.Focused.Text = fg(m.st.Text)
	ss.Focused.Placeholder = fg(m.st.Muted)
	ss.Cursor.Color = m.st.Accent2
	in.SetStyles(ss)
	p := &paletteModal{title: title, input: in, all: m.commands(), sc: newScroller()}
	p.filter()
	return m.open(p, p.input.Focus())
}

func (p *paletteModal) Width(m *Model) int { return min(72, m.w-4) }

func (p *paletteModal) filter() {
	q := strings.ToLower(strings.TrimSpace(p.input.Value()))
	type scored struct{ i, s int }
	var res []scored
	for i, c := range p.all {
		hay := strings.ToLower(c.group + " " + c.title)
		if sc, ok := fuzzy(hay, q); ok {
			res = append(res, scored{i, sc})
		}
	}
	sort.SliceStable(res, func(a, b int) bool { return res[a].s > res[b].s })
	p.matches = p.matches[:0]
	for _, r := range res {
		p.matches = append(p.matches, r.i)
	}
	p.sel = 0
	p.sc.Snap(0)
}

// fuzzy returns a score for subsequence matches, favouring contiguous and
// word-start hits.
func fuzzy(hay, q string) (int, bool) {
	if q == "" {
		return 0, true
	}
	if i := strings.Index(hay, q); i >= 0 {
		return 1000 - i, true
	}
	score, hi, prev := 0, 0, -2
	hr := []rune(hay)
	for _, qc := range q {
		found := false
		for ; hi < len(hr); hi++ {
			if hr[hi] == qc {
				score += 10
				if hi == prev+1 {
					score += 15
				}
				if hi == 0 || hr[hi-1] == ' ' || hr[hi-1] == '/' || hr[hi-1] == '-' {
					score += 20
				}
				prev = hi
				hi++
				found = true
				break
			}
		}
		if !found {
			return 0, false
		}
	}
	return score, true
}

func (p *paletteModal) Update(m *Model, msg tea.Msg) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			return true, nil
		case "up", "ctrl+k", "ctrl+p":
			p.move(-1)
			return false, m.kick()
		case "down", "ctrl+j", "ctrl+n", "tab":
			p.move(1)
			return false, m.kick()
		case "enter":
			if len(p.matches) == 0 {
				return true, nil
			}
			c := p.all[p.matches[p.sel]]
			m.modal = nil
			return true, c.run(m)
		}
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			p.move(-1)
		} else {
			p.move(1)
		}
		return false, m.kick()
	}
	var cmd tea.Cmd
	before := p.input.Value()
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != before {
		p.filter()
	}
	return false, cmd
}

func (p *paletteModal) move(d int) {
	if len(p.matches) == 0 {
		return
	}
	p.sel = clamp(p.sel+d, 0, len(p.matches)-1)
	p.sc.Follow(p.sel, paletteRows, len(p.matches))
}

func (p *paletteModal) View(m *Model) (string, tui.View) {
	w := p.Width(m) - 6
	p.input.SetWidth(w - 3)
	rows := tui.List(len(p.matches), p.sel, &p.sc, func(i int, sel bool) tui.View {
		c := p.all[p.matches[i]]
		bar, title := tui.Text("  "), tui.Text(c.title).Fg(tui.Secondary)
		if sel {
			bar, title = tui.Text("▌ ").Fg(tui.Accent2), tui.Text(c.title).Bold()
		}
		return tui.HStack(
			bar,
			tui.Width(9, tui.Text(c.group).Fg(tui.Muted)),
			title.Grow(),
			tui.If(c.key != "", tui.Text(c.key+" ").Fg(tui.Accent).Bold()),
		)
	}).Highlight(tui.Selection)
	return p.title, tui.VStack(
		tui.Raw(p.input.View()),
		tui.Divider(),
		tui.Height(paletteRows, rows),
		tui.Textf("%d results · ↑↓ select · ⏎ run · esc close", len(p.matches)).Fg(tui.Muted),
	)
}

// ─── Theme picker ───────────────────────────────────────────────────────────

type themeModal struct {
	orig     theme.Theme
	origTerm bool
	sel      int
}

func (m *Model) openThemes() tea.Cmd {
	tm := &themeModal{orig: m.st.T, origTerm: m.termMode}
	for i, t := range m.themes {
		if t.Name == m.st.T.Name {
			tm.sel = i
		}
	}
	return m.open(tm, nil)
}

func (t *themeModal) Width(m *Model) int { return min(64, m.w-4) }

func (t *themeModal) Update(m *Model, msg tea.Msg) (bool, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false, nil
	}
	switch k.String() {
	case "esc", "q":
		m.termMode = t.origTerm
		m.applyTheme(t.orig)
		return true, nil
	case "up", "k":
		t.sel = (t.sel - 1 + len(m.themes)) % len(m.themes)
		m.usePresetTheme(m.themes[t.sel])
	case "down", "j":
		t.sel = (t.sel + 1) % len(m.themes)
		m.usePresetTheme(m.themes[t.sel])
	case "enter":
		m.cfg.Theme = m.themes[t.sel].Name
		err := config.Save(m.cfg)
		if err != nil {
			return true, m.toast("Couldn't save theme", err.Error(), toastError)
		}
		return true, m.toast("Theme · "+m.cfg.Theme, "saved to "+config.Dir(), toastSuccess)
	}
	return false, nil
}

func (t *themeModal) View(m *Model) (string, tui.View) {
	rows := tui.ForEach(m.themes, func(i int, th theme.Theme) tui.View {
		var swatch []tui.Span
		for _, c := range []string{th.Bg, th.Accent, th.Accent2, th.Accent3, th.Success, th.Warning, th.Error} {
			swatch = append(swatch, tui.S("██", tui.Fg(tui.Hex(c))))
		}
		label := th.Name
		if th.Name == termsync.ThemeName() {
			label += " (live)"
		}
		bar, name := tui.Text("  "), tui.Text(label).Fg(tui.Secondary)
		if i == t.sel {
			bar, name = tui.Text("▌ ").Fg(tui.Accent2), tui.Text(label).Bold()
		}
		return tui.HStack(bar, tui.Width(20, name), tui.Line(swatch...))
	})
	return "Themes", tui.VStack(
		rows,
		tui.Text(""),
		tui.Text("↑↓ preview live · ⏎ keep · esc revert").Fg(tui.Muted),
		tui.Text("custom themes: "+config.ThemesDir()+"/*.json").Fg(tui.Muted),
	)
}

// ─── Toasts ─────────────────────────────────────────────────────────────────

const toastWidth = 44

type toastKind int

const (
	toastSuccess toastKind = iota
	toastError
	toastInfo
)

type toast struct {
	id      int
	title   string
	body    string
	kind    toastKind
	x       spring
	leaving bool
}

func (m *Model) toast(title, body string, kind toastKind) tea.Cmd {
	m.toastSeq++
	t := &toast{id: m.toastSeq, title: title, body: body, kind: kind, x: newSpring(&popSpring, toastWidth+4)}
	t.x.Set(0)
	m.toasts = append(m.toasts, t)
	if len(m.toasts) > 4 {
		m.toasts = m.toasts[len(m.toasts)-4:]
	}
	ttl := 3500 * time.Millisecond
	if kind == toastError {
		ttl = 8 * time.Second
	}
	id := t.id
	return tea.Batch(m.kick(), tea.Tick(ttl, func(time.Time) tea.Msg { return toastExpireMsg{id} }))
}

// toastLayers stacks toasts in the bottom-right corner, newest at the
// bottom, each sliding in on its spring.
func (m *Model) toastLayers() []tui.Layer {
	var out []tui.Layer
	ty := m.h - 1
	for i := len(m.toasts) - 1; i >= 0; i-- {
		t := m.toasts[i]
		card, h := toastCard(t)
		ty -= h
		if ty < 1 {
			break
		}
		x := m.w - toastWidth - 2 + t.x.Int()
		if x < m.w-1 {
			out = append(out, tui.At(tui.Start, tui.Start, card).Offset(x, ty))
		}
	}
	return out
}

// toastCard is a toast as a bordered card and its height.
func toastCard(t *toast) (tui.View, int) {
	c, icon := tui.Color(tui.Success), "✓"
	switch t.kind {
	case toastError:
		c, icon = tui.Danger, "✗"
	case toastInfo:
		c, icon = tui.Info, "i"
	}
	lines := []tui.View{tui.Line(
		tui.S(icon+" ", tui.Fg(c).Bold()),
		tui.S(truncate(t.title, toastWidth-6), tui.Fg(tui.Primary).Bold()),
	)}
	if t.body != "" {
		body := textx.Wrap(t.body, toastWidth-4)
		if len(body) > 4 {
			body = append(body[:3], "…")
		}
		for _, l := range body {
			lines = append(lines, tui.Text(l).Fg(tui.Secondary))
		}
	}
	h := len(lines) + 2
	return tui.Frame(toastWidth, h, tui.Panel(tui.Pad(tui.VStack(lines...), 0, 1)).BorderColor(c)), h
}

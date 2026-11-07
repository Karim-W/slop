package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/karim-w/gitmon/internal/theme"
)

type modal interface {
	Update(m *Model, msg tea.Msg) (closed bool, cmd tea.Cmd)
	View(m *Model) (title, body string)
	Width(m *Model) int
}

func (m *Model) open(md modal, init tea.Cmd) tea.Cmd {
	m.modal = md
	m.modalY.snap(-4)
	m.modalY.set(0)
	return tea.Batch(init, m.kick())
}

func (m *Model) renderModal() string {
	s := m.st
	title, body := m.modal.View(m)
	w := m.modal.Width(m)
	head := gradient(" "+title+" ", true, s.Accent, s.Accent2, s.Accent3)
	content := head + "\n\n" + body
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForegroundBlend(s.Accent, s.Accent2, s.Accent3).
		Padding(1, 2).
		Width(w).
		Render(content)
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

func (f *formModal) View(m *Model) (string, string) {
	return f.title, strings.TrimRight(f.form.View(), "\n ")
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
	p.sc.snap(0)
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
	p.sc.follow(p.sel, paletteRows, len(p.matches))
}

func (p *paletteModal) View(m *Model) (string, string) {
	s := m.st
	w := p.Width(m) - 6
	p.input.SetWidth(w - 3)
	var L []string
	L = append(L, p.input.View(), fg(s.Border).Render(strings.Repeat("─", w)))
	off := p.sc.offset(paletteRows, len(p.matches))
	for i := off; i < min(len(p.matches), off+paletteRows); i++ {
		c := p.all[p.matches[i]]
		sel := i == p.sel
		var bg = s.Sel
		if !sel {
			bg = nil
		}
		grp := on(s.Muted, bg).Render(pad(c.group, 9))
		key := ""
		if c.key != "" {
			key = on(s.Accent, bg).Bold(true).Render(c.key) + on(s.Text, bg).Render(" ")
		}
		bar := on(s.Text, bg).Render("  ")
		title := on(s.Sub, bg).Render(c.title)
		if sel {
			bar = on(s.Accent2, bg).Render("▌ ")
			title = on(s.Text, bg).Bold(true).Render(c.title)
		}
		left := bar + grp + title
		L = append(L, padBg(left, w-lipgloss.Width(key), bg)+key)
	}
	for len(L) < paletteRows+2 {
		L = append(L, "")
	}
	L = append(L, fg(s.Muted).Render(fmt.Sprintf("%d results · ↑↓ select · ⏎ run · esc close", len(p.matches))))
	return p.title, strings.Join(L, "\n")
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
		err := theme.SaveConfig(m.cfg)
		if err != nil {
			return true, m.toast("Couldn't save theme", err.Error(), toastError)
		}
		return true, m.toast("Theme · "+m.cfg.Theme, "saved to "+theme.ConfigDir(), toastSuccess)
	}
	return false, nil
}

func (t *themeModal) View(m *Model) (string, string) {
	s := m.st
	var L []string
	for i, th := range m.themes {
		sw := ""
		for _, c := range []string{th.Bg, th.Accent, th.Accent2, th.Accent3, th.Success, th.Warning, th.Error} {
			sw += lipgloss.NewStyle().Foreground(theme.Hex(c)).Render("██")
		}
		label := th.Name
		if th.Name == theme.TerminalName() {
			label += " (live)"
		}
		name := pad(label, 20)
		if i == t.sel {
			L = append(L, fg(s.Accent2).Render("▌ ")+fg(s.Text).Bold(true).Render(name)+sw)
		} else {
			L = append(L, "  "+fg(s.Sub).Render(name)+sw)
		}
	}
	L = append(L, "", fg(s.Muted).Render("↑↓ preview live · ⏎ keep · esc revert"),
		fg(s.Muted).Render("custom themes: "+theme.ConfigDir()+"/themes/*.json"))
	return "Themes", strings.Join(L, "\n")
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
	t.x.set(0)
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

func (m *Model) renderToast(t *toast) string {
	s := m.st
	c, icon := s.Success, "✓"
	switch t.kind {
	case toastError:
		c, icon = s.Error, "✗"
	case toastInfo:
		c, icon = s.Info, "i"
	}
	body := fg(c).Bold(true).Render(icon+" ") + fg(s.Text).Bold(true).Render(truncate(t.title, toastWidth-6))
	if t.body != "" {
		lines := wrap(t.body, toastWidth-4)
		if len(lines) > 4 {
			lines = append(lines[:3], "…")
		}
		for _, l := range lines {
			body += "\n" + fg(s.Sub).Render(l)
		}
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c).
		Padding(0, 1).
		Width(toastWidth).
		Render(body)
}

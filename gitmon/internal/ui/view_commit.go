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

// ─── Floating commit view ───────────────────────────────────────────────────

type commitModal struct{}

func (m *Model) openCommit() tea.Cmd {
	c := m.selectedCommit()
	if c.WIP {
		m.focus = boxTree
		return nil
	}
	if c.Hash == "" {
		return nil
	}
	m.insFocus = 0
	m.insFiles.sel = 0
	m.insFiles.snap(0)
	m.insDiff.clear()
	return m.open(&commitModal{}, m.loadInspectDiff())
}

func (cm *commitModal) Width(m *Model) int { return max(60, m.w*88/100) }

func (cm *commitModal) height(m *Model) int { return max(10, m.h*82/100-6) }

func (cm *commitModal) Update(m *Model, msg tea.Msg) (bool, tea.Cmd) {
	h := cm.height(m) - 4
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		d := 3
		if msg.Button == tea.MouseWheelUp {
			d = -3
		}
		m.insDiff.scroll(d, h)
		return false, m.kick()
	case tea.KeyPressMsg:
		key := msg.String()
		d := m.details[m.selectedCommit().Hash]
		switch key {
		case "esc", "q":
			return true, nil
		case "tab", "shift+tab":
			m.insFocus = 1 - m.insFocus
			return false, nil
		case "[", "]", "ctrl+left", "ctrl+right":
			delta := 1 // older
			if key == "]" || key == "ctrl+right" {
				delta = -1
			}
			i := clamp(m.sel+delta, 0, len(m.commits)-1)
			if m.commits[i].WIP {
				return false, nil
			}
			m.sel = i
			m.followGraph(false)
			m.insFiles.sel = 0
			m.insFiles.snap(0)
			m.insDiff.clear()
			return false, tea.Batch(m.loadInspectDiff(), m.kick())
		case "y":
			c := m.selectedCommit()
			return false, tea.Batch(tea.SetClipboard(c.Hash), m.toast("Copied "+c.Short, c.Hash, toastInfo))
		case "e":
			if d != nil && len(d.Files) > 0 {
				return true, m.editFile(d.Files[m.insFiles.sel].Path)
			}
		}
		if m.insFocus == 1 {
			if key == "h" || key == "left" {
				m.insFocus = 0
				return false, nil
			}
			return false, m.diffKey(&m.insDiff, key, h)
		}
		if d == nil {
			return false, nil
		}
		switch key {
		case "l", "right", "enter":
			m.insFocus = 1
			return false, nil
		}
		if i, ok := listMove(key, m.insFiles.sel, len(d.Files), h/2); ok {
			m.insFiles.sel = i
			m.insFiles.follow(i, h-2, len(d.Files))
			return false, tea.Batch(m.loadInspectDiff(), m.kick())
		}
	}
	return false, nil
}

func (cm *commitModal) View(m *Model) (string, string) {
	s := m.st
	m.rs = m.st
	c := m.selectedCommit()
	d := m.details[c.Hash]
	W := cm.Width(m) - 6
	H := cm.height(m)
	lc := s.lane(m.rows[m.sel].Color)

	// header
	var head []string
	head = append(head, spread(s.avatar(c.Author, nil)+" "+fg(s.Text).Bold(true).Render(c.Author)+fg(s.Muted).Render("  "+c.Email),
		fg(s.Sub).Render(relTimeLong(c.Time))+fg(s.Muted).Render(" · "+c.Time.Format("Mon Jan 2 2006 15:04")), W))
	head = append(head, fg(s.Text).Bold(true).Render(truncate(c.Subject, W)))
	if refs := m.refChipsInline(c, lc); refs != "" {
		head = append(head, truncate(refs, W))
	}
	if d != nil && d.Body != "" {
		bl := wrap(d.Body, W)
		if len(bl) > 3 {
			bl = append(bl[:2], "…")
		}
		for _, l := range bl {
			head = append(head, fg(s.Sub).Render(l))
		}
	}
	head = append(head, fg(s.Border).Render(strings.Repeat("─", W)))
	bodyH := H - len(head)

	lw := clamp(W*32/100, 26, 50)
	rw := W - lw - 1
	var L []string
	if d == nil {
		L = append(L, fg(s.Muted).Render(" loading…"))
	} else {
		adds, dels := 0, 0
		for _, f := range d.Files {
			adds += f.Added
			dels += f.Deleted
		}
		dot := fg(s.Border).Render("○ ")
		if m.insFocus == 0 {
			dot = fg(s.Accent2).Render("● ")
		}
		L = append(L, dot+fg(s.Accent).Bold(true).Render(fmt.Sprintf("%d FILES ", len(d.Files)))+fg(s.Added).Render(fmt.Sprintf("+%d ", adds))+fg(s.Removed).Render(fmt.Sprintf("−%d", dels)))
		listH := bodyH - 1
		off := m.insFiles.offset(listH, len(d.Files))
		for i := off; i < min(len(d.Files), off+listH); i++ {
			f := d.Files[i]
			var bg color.Color
			bar := "  "
			if i == m.insFiles.sel {
				bc := s.Border
				if m.insFocus == 0 {
					bc, bg = s.Accent2, s.Sel
				}
				bar = on(bc, bg).Render("▌ ")
			}
			L = append(L, padBg(bar+m.changeLine2(f, lw-3, bg), lw, bg))
		}
	}
	left := fitBlock(strings.Join(L, "\n"), lw, bodyH)

	title := ""
	if d != nil && len(d.Files) > 0 {
		f := d.Files[clamp(m.insFiles.sel, 0, len(d.Files)-1)]
		dot := fg(s.Border).Render("○ ")
		if m.insFocus == 1 {
			dot = fg(s.Accent2).Render("● ")
		}
		title = dot + fg(s.Text).Bold(true).Render(f.Path)
	}
	right := m.renderDiff(&m.insDiff, rw, bodyH, m.insFocus == 1, false, title)
	sep := strings.TrimSuffix(strings.Repeat(fg(s.Border).Render("│")+"\n", bodyH), "\n")
	body := strings.Join(head, "\n") + "\n" + hcat(bodyH, col{left, lw}, col{sep, 1}, col{right, rw})
	hint := fg(s.Muted).Render("↑↓ files · tab diff · n/N hunks · [ ] older/newer commit · y copy sha · e edit · esc close")
	body += "\n" + truncate(hint, W)
	return "Commit " + c.Short, body
}

func (m *Model) changeLine2(f git.FileChange, w int, bg color.Color) string {
	s := m.st
	dir, base := filepath.Split(f.Path)
	stat := ""
	if f.Binary {
		stat = on(s.Muted, bg).Render("bin")
	} else {
		stat = on(s.Added, bg).Render(fmt.Sprintf("+%d", f.Added)) + on(s.Removed, bg).Render(fmt.Sprintf(" −%d", f.Deleted))
	}
	name := on(m.statusColor(f.Status), bg).Bold(true).Render(string(f.Status)) + on(s.Text, bg).Render(" "+base)
	if dir != "" {
		name += on(s.Muted, bg).Render(" " + strings.TrimSuffix(dir, "/"))
	}
	nw := w - lipgloss.Width(stat) - 1
	return padBg(truncate(name, nw), nw, bg) + on(s.Text, bg).Render(" ") + stat
}

// ─── Generic list modal (branches, stashes) ─────────────────────────────────

type listModal struct {
	title string
	width int
	sel   int
	sc    scroller
	items func(m *Model) []string // pre-rendered rows (without selection bar)
	count func(m *Model) int
	onKey func(m *Model, key string, sel int) (bool, tea.Cmd)
	hint  string
}

const listRows = 16

func (l *listModal) Width(m *Model) int { return min(l.width, m.w-4) }

func (l *listModal) Update(m *Model, msg tea.Msg) (bool, tea.Cmd) {
	n := l.count(m)
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			l.sel = clamp(l.sel-1, 0, max(0, n-1))
		} else {
			l.sel = clamp(l.sel+1, 0, max(0, n-1))
		}
		l.sc.follow(l.sel, listRows, n)
		return false, m.kick()
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "esc" || key == "q" {
			return true, nil
		}
		if i, ok := listMove(key, l.sel, n, listRows/2); ok {
			l.sel = i
			l.sc.follow(i, listRows, n)
			return false, m.kick()
		}
		if n == 0 {
			return l.onKey(m, key, -1)
		}
		return l.onKey(m, key, clamp(l.sel, 0, n-1))
	}
	return false, nil
}

func (l *listModal) View(m *Model) (string, string) {
	s := m.st
	m.rs = m.st
	w := l.Width(m) - 6
	rows := l.items(m)
	l.sel = clamp(l.sel, 0, max(0, len(rows)-1))
	var L []string
	if len(rows) == 0 {
		L = append(L, fg(s.Muted).Render("  nothing here yet"))
	}
	off := l.sc.offset(listRows, len(rows))
	for i := off; i < min(len(rows), off+listRows); i++ {
		if i == l.sel {
			L = append(L, padBg(on(s.Accent2, s.Sel).Render("▌ ")+rows[i], w, s.Sel))
		} else {
			L = append(L, pad("  "+rows[i], w))
		}
	}
	for len(L) < min(listRows, max(len(rows), 1)) {
		L = append(L, "")
	}
	L = append(L, "", fg(s.Muted).Render(truncate(l.hint, w)))
	return l.title, strings.Join(L, "\n")
}

func (m *Model) openBranches() tea.Cmd {
	lm := &listModal{
		title: "Branches",
		width: 84,
		sc:    newScroller(),
		hint:  "⏎ checkout · n new from · m merge · r rebase onto · R rename · D delete · y copy · esc close",
		count: func(m *Model) int { return len(m.branches) },
		items: func(m *Model) []string {
			s := m.st
			var out []string
			for _, b := range m.branches {
				lane := s.lane(hashIndex(b.Name))
				icon := fg(lane).Render("● ")
				name := fg(s.Text).Render(b.Name)
				if b.Head {
					icon = fg(s.Success).Bold(true).Render("◉ ")
					name = fg(s.Success).Bold(true).Render(b.Name)
				} else if b.Remote {
					icon = fg(lane).Render("☁ ")
					name = fg(s.Sub).Render(b.Name)
				}
				x := icon + name
				if b.Ahead > 0 {
					x += fg(s.Success).Render(fmt.Sprintf(" ↑%d", b.Ahead))
				}
				if b.Behind > 0 {
					x += fg(s.Warning).Render(fmt.Sprintf(" ↓%d", b.Behind))
				}
				if b.Gone {
					x += fg(s.Error).Render(" ✗ gone")
				}
				x += fg(s.Muted).Render("  " + truncate(b.Subject, 34) + " · " + relTime(b.Date))
				out = append(out, x)
			}
			return out
		},
	}
	for i, b := range m.branches {
		if b.Head {
			lm.sel = i
			lm.sc.follow(i, listRows, len(m.branches))
			lm.sc.snap(lm.sc.target)
		}
	}
	lm.onKey = func(m *Model, key string, sel int) (bool, tea.Cmd) {
		if sel < 0 || sel >= len(m.branches) {
			return false, nil
		}
		b := m.branches[sel]
		switch key {
		case "enter", "space":
			if b.Head {
				return true, nil
			}
			if b.Remote {
				return true, m.run("Checked out "+b.Name, func() error { return m.repo.CheckoutRemote(b.Name) })
			}
			return true, m.run("Switched to "+b.Name, func() error { return m.repo.Checkout(b.Name) })
		case "n":
			return true, m.prompt("New branch", "Branch name (from "+b.Name+")", "feature/something", "", func(name string) tea.Cmd {
				return m.run("Created "+name, func() error { return m.repo.CreateBranch(name, b.Name, true) })
			})
		case "m":
			return true, m.mergeConfirm(b.Name)
		case "r":
			return true, m.confirm("Rebase", fmt.Sprintf("Rebase %s onto %s?", m.currentBranch(), b.Name), func() tea.Cmd {
				return m.run("Rebased onto "+b.Name, func() error { return m.repo.Rebase(b.Name) })
			})
		case "R":
			if b.Remote {
				return false, nil
			}
			return true, m.prompt("Rename branch", "New name for "+b.Name, b.Name, b.Name, func(name string) tea.Cmd {
				return m.run("Renamed to "+name, func() error { return m.repo.RenameBranch(b.Name, name) })
			})
		case "D", "d":
			if b.Head {
				return false, m.toast("Can't delete", "that's the branch you're on", toastInfo)
			}
			if b.Remote {
				return true, m.confirm("Delete remote branch", fmt.Sprintf("Push a deletion of %s to the remote?", b.Name), func() tea.Cmd {
					return m.run("Deleted "+b.Name, func() error { return m.repo.DeleteRemoteBranch(b.Name) })
				})
			}
			return true, m.confirm("Delete branch", fmt.Sprintf("Delete %s? Unmerged commits will be lost.", b.Name), func() tea.Cmd {
				return m.run("Deleted "+b.Name, func() error { return m.repo.DeleteBranch(b.Name, true) })
			})
		case "y":
			return false, tea.Batch(tea.SetClipboard(b.Name), m.toast("Copied "+b.Name, "", toastInfo))
		}
		return false, nil
	}
	return m.open(lm, nil)
}

func (m *Model) openStashes() tea.Cmd {
	lm := &listModal{
		title: "Stashes",
		width: 76,
		sc:    newScroller(),
		hint:  "⏎/p pop · a apply · D drop · s stash now · esc close",
		count: func(m *Model) int { return len(m.stashes) },
		items: func(m *Model) []string {
			s := m.st
			var out []string
			for _, st := range m.stashes {
				msg := st.Message
				if strings.HasPrefix(msg, "On ") || strings.HasPrefix(msg, "WIP on ") {
					if j := strings.Index(msg, ": "); j >= 0 {
						msg = msg[j+2:]
					}
				}
				out = append(out, fg(s.Accent).Render(fmt.Sprintf("%d  ", st.Index))+fg(s.Text).Render(msg)+fg(s.Muted).Render("  "+relTime(st.Date)))
			}
			return out
		},
	}
	lm.onKey = func(m *Model, key string, sel int) (bool, tea.Cmd) {
		if key == "s" {
			return true, m.stashForm()
		}
		if sel < 0 || sel >= len(m.stashes) {
			return false, nil
		}
		st := m.stashes[sel]
		switch key {
		case "enter", "p":
			return true, m.run("Popped "+st.Ref, func() error { return m.repo.StashPop(st.Ref) })
		case "a", "space":
			return true, m.run("Applied "+st.Ref, func() error { return m.repo.StashApply(st.Ref) })
		case "D", "d":
			return true, m.confirm("Drop stash", fmt.Sprintf("Drop %s for good?", st.Ref), func() tea.Cmd {
				return m.run("Dropped "+st.Ref, func() error { return m.repo.StashDrop(st.Ref) })
			})
		}
		return false, nil
	}
	return m.open(lm, nil)
}

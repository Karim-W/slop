package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/gitmon/internal/git"
	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/utils/textx"
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
	m.insFiles.Snap(0)
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
			m.insFiles.Snap(0)
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
			m.insFiles.Follow(i, h-2, len(d.Files))
			return false, tea.Batch(m.loadInspectDiff(), m.kick())
		}
	}
	return false, nil
}

func (cm *commitModal) View(m *Model) (string, tui.View) {
	s := m.st
	c := m.selectedCommit()
	d := m.details[c.Hash]
	W := cm.Width(m) - 6
	H := cm.height(m)
	lc := s.Lane(m.rows[m.sel].Color)

	// header: who and when, subject, refs, the start of the body
	head := []tui.View{
		tui.Raw(spread(avatar(s, c.Author, nil)+" "+fg(s.Text).Bold(true).Render(c.Author)+fg(s.Muted).Render("  "+c.Email),
			fg(s.Sub).Render(relTimeLong(c.Time))+fg(s.Muted).Render(" · "+c.Time.Format("Mon Jan 2 2006 15:04")), W)),
		tui.Text(c.Subject).Bold(),
	}
	if refs := refChipsInline(s, c, lc); refs != "" {
		head = append(head, tui.Raw(refs))
	}
	if d != nil && d.Body != "" {
		bl := textx.Wrap(d.Body, W)
		if len(bl) > 3 {
			bl = append(bl[:2], "…")
		}
		for _, l := range bl {
			head = append(head, tui.Text(l).Fg(tui.Secondary))
		}
	}
	head = append(head, tui.Divider())
	bodyH := H - len(head)

	lw := clamp(W*32/100, 26, 50)
	var title []tui.Span
	if d != nil && len(d.Files) > 0 {
		f := d.Files[clamp(m.insFiles.sel, 0, len(d.Files)-1)]
		title = []tui.Span{focusDot(m.insFocus == 1), tui.S(f.Path, tui.Fg(tui.Primary).Bold())}
	}
	return "Commit " + c.Short, tui.VStack(
		tui.Group(head...),
		tui.Height(bodyH, tui.HStack(
			tui.Width(lw, m.inspectFiles(d)),
			tui.VDivider(),
			m.diffView(&m.insDiff, m.insFocus == 1, false, title),
		)),
		tui.Text("↑↓ files · tab diff · n/N hunks · [ ] older/newer commit · y copy sha · e edit · esc close").Fg(tui.Muted),
	)
}

// focusDot marks which side of the inspector has the keyboard.
func focusDot(on bool) tui.Span {
	if on {
		return tui.S("● ", tui.Fg(tui.Accent2))
	}
	return tui.S("○ ", tui.Fg(tui.Border))
}

// inspectFiles is the inspector's file list with its totals on top.
func (m *Model) inspectFiles(d *git.CommitDetail) tui.View {
	if d == nil {
		return tui.Text(" loading…").Fg(tui.Muted)
	}
	adds, dels := 0, 0
	for _, f := range d.Files {
		adds += f.Added
		dels += f.Deleted
	}
	focused := m.insFocus == 0
	list := tui.List(len(d.Files), m.insFiles.sel, &m.insFiles, func(i int, sel bool) tui.View {
		bar := tui.Text("  ")
		if sel {
			bar = tui.Text("▌ ").Fg(tui.Border)
			if focused {
				bar = tui.Text("▌ ").Fg(tui.Accent2)
			}
		}
		return tui.HStack(bar, m.changeLine2(d.Files[i]))
	})
	if focused {
		list = list.Highlight(tui.Selection)
	}
	return tui.VStack(
		tui.Line(
			focusDot(focused),
			tui.S(fmt.Sprintf("%d FILES ", len(d.Files)), tui.Fg(tui.Accent).Bold()),
			tui.S(fmt.Sprintf("+%d ", adds), tui.Fg(tui.Added)),
			tui.S(fmt.Sprintf("−%d", dels), tui.Fg(tui.Removed)),
		),
		list,
	)
}

// changeLine2 is one changed file: status, name and folder, then +/− counts.
func (m *Model) changeLine2(f git.FileChange) tui.View {
	stat := tui.Line(tui.S(fmt.Sprintf("+%d", f.Added), tui.Fg(tui.Added)), tui.S(fmt.Sprintf(" −%d", f.Deleted), tui.Fg(tui.Removed)))
	if f.Binary {
		stat = tui.Line(tui.S("bin", tui.Fg(tui.Muted)))
	}
	return tui.HStack(tui.Line(fileSpans(f.Status, f.Path, tui.Primary)...).Grow(), tui.Text(" "), stat, tui.Text(" "))
}

// ─── Generic list modal (branches, stashes) ─────────────────────────────────

type listModal struct {
	title string
	width int
	sel   int
	sc    scroller
	items func(m *Model) []tui.View // rows, without the selection bar
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
		l.sc.Follow(l.sel, listRows, n)
		return false, m.kick()
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "esc" || key == "q" {
			return true, nil
		}
		if i, ok := listMove(key, l.sel, n, listRows/2); ok {
			l.sel = i
			l.sc.Follow(i, listRows, n)
			return false, m.kick()
		}
		if n == 0 {
			return l.onKey(m, key, -1)
		}
		return l.onKey(m, key, clamp(l.sel, 0, n-1))
	}
	return false, nil
}

func (l *listModal) View(m *Model) (string, tui.View) {
	rows := l.items(m)
	l.sel = clamp(l.sel, 0, max(0, len(rows)-1))
	list := tui.List(len(rows), l.sel, &l.sc, func(i int, sel bool) tui.View {
		bar := tui.Text("  ")
		if sel {
			bar = tui.Text("▌ ").Fg(tui.Accent2)
		}
		return tui.HStack(bar, rows[i])
	}).Highlight(tui.Selection).Empty(tui.Text("  nothing here yet").Fg(tui.Muted))
	return l.title, tui.VStack(
		tui.Height(min(listRows, max(len(rows), 1)), list),
		tui.Text(""),
		tui.Text(l.hint).Fg(tui.Muted),
	)
}

func (m *Model) openBranches() tea.Cmd {
	lm := &listModal{
		title: "Branches",
		width: 84,
		sc:    newScroller(),
		hint:  "⏎ checkout · n new from · m merge · r rebase onto · R rename · D delete · y copy · esc close",
		count: func(m *Model) int { return len(m.branches) },
		items: func(m *Model) []tui.View {
			var out []tui.View
			for _, b := range m.branches {
				lane := tui.Lane(hashIndex(b.Name))
				icon, name := tui.S("● ", tui.Fg(lane)), tui.S(b.Name, tui.Fg(tui.Primary))
				if b.Head {
					icon, name = tui.S("◉ ", tui.Fg(tui.Success).Bold()), tui.S(b.Name, tui.Fg(tui.Success).Bold())
				} else if b.Remote {
					icon, name = tui.S("☁ ", tui.Fg(lane)), tui.S(b.Name, tui.Fg(tui.Secondary))
				}
				x := []tui.Span{icon, name}
				if b.Ahead > 0 {
					x = append(x, tui.S(fmt.Sprintf(" ↑%d", b.Ahead), tui.Fg(tui.Success)))
				}
				if b.Behind > 0 {
					x = append(x, tui.S(fmt.Sprintf(" ↓%d", b.Behind), tui.Fg(tui.Warning)))
				}
				if b.Gone {
					x = append(x, tui.S(" ✗ gone", tui.Fg(tui.Danger)))
				}
				x = append(x, tui.S("  "+truncate(b.Subject, 34)+" · "+relTime(b.Date), tui.Fg(tui.Muted)))
				out = append(out, tui.Line(x...))
			}
			return out
		},
	}
	for i, b := range m.branches {
		if b.Head {
			lm.sel = i
			lm.sc.Follow(i, listRows, len(m.branches))
			lm.sc.Snap(lm.sc.Target)
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
		items: func(m *Model) []tui.View {
			var out []tui.View
			for _, st := range m.stashes {
				msg := st.Message
				if strings.HasPrefix(msg, "On ") || strings.HasPrefix(msg, "WIP on ") {
					if j := strings.Index(msg, ": "); j >= 0 {
						msg = msg[j+2:]
					}
				}
				out = append(out, tui.Line(
					tui.S(fmt.Sprintf("%d  ", st.Index), tui.Fg(tui.Accent)),
					tui.S(msg, tui.Fg(tui.Primary)),
					tui.S("  "+relTime(st.Date), tui.Fg(tui.Muted)),
				))
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

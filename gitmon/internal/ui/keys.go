package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/karim-w/gitmon/internal/git"
	"github.com/karim-w/gitmon/internal/theme"
)

func (m *Model) onKey(k tea.KeyPressMsg) tea.Cmd {
	key := k.String()
	switch key {
	case "ctrl+c", "q":
		return m.whenQuiet(tea.Quit)
	case "tab":
		m.focus = (m.focus + 1) % 3
		return nil
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
		return nil
	case "1":
		m.focus = boxTree
		return nil
	case "2":
		m.focus = boxDiff
		return nil
	case "3":
		m.focus = boxGraph
		return nil
	case "ctrl+p", ":":
		return m.openPalette("Commands")
	case "?":
		return m.openPalette("Keys & commands")
	case "T":
		return m.openThemes()
	case "B":
		return m.openBranches()
	case "Z":
		return m.openStashes()
	case "ctrl+r":
		return m.loadAll()
	case "f":
		return m.run("Fetched", m.repo.Fetch)
	case "p":
		return m.run("Pulled", m.repo.Pull)
	case "P":
		return m.push(false)
	case "S":
		return m.stashForm()
	case "c":
		return m.commitForm(false)
	case "A":
		return m.commitForm(true)
	}
	switch m.focus {
	case boxTree:
		return m.treeKey(key)
	case boxDiff:
		return m.diffBoxKey(key)
	case boxGraph:
		return m.graphKey(key)
	}
	return nil
}

func listMove(key string, sel, n, page int) (int, bool) {
	switch key {
	case "j", "down":
		return clamp(sel+1, 0, max(0, n-1)), true
	case "k", "up":
		return clamp(sel-1, 0, max(0, n-1)), true
	case "g", "home":
		return 0, true
	case "G", "end":
		return max(0, n-1), true
	case "ctrl+d", "pgdown":
		return clamp(sel+page, 0, max(0, n-1)), true
	case "ctrl+u", "pgup":
		return clamp(sel-page, 0, max(0, n-1)), true
	}
	return sel, false
}

func (m *Model) diffKey(p *diffPane, key string, h int) tea.Cmd {
	switch key {
	case "j", "down":
		p.scroll(1, h)
	case "k", "up":
		p.scroll(-1, h)
	case "ctrl+d", "pgdown":
		p.scroll(h/2, h)
	case "ctrl+u", "pgup":
		p.scroll(-h/2, h)
	case "g", "home":
		p.off.set(0)
	case "G", "end":
		p.scroll(1<<20, h)
	case "n", "}":
		p.jumpHunk(1, h)
	case "N", "{":
		p.jumpHunk(-1, h)
	default:
		return nil
	}
	return m.kick()
}

// ─── Working tree ───────────────────────────────────────────────────────────

func (m *Model) treeSelect(i int) tea.Cmd {
	tr, _, _ := m.layout()
	m.ch.sel = clamp(i, 0, max(0, m.itemCount()-1))
	m.ch.follow(m.changeLine(m.ch.sel), tr.inner().h, m.treeLines())
	return tea.Batch(m.loadChangeDiff(false), m.kick())
}

func (m *Model) setCollapsed(r treeRow, v bool) tea.Cmd {
	if r.kind != rowDir {
		return nil
	}
	if v {
		m.collapsed[r.path] = true
	} else {
		delete(m.collapsed, r.path)
	}
	m.rebuildTree()
	return m.treeSelect(m.ch.sel)
}

func (m *Model) toggleTreeMode() tea.Cmd {
	m.flatTree = !m.flatTree
	m.cfg.FlatTree = m.flatTree
	_ = theme.SaveConfig(m.cfg)
	m.rebuildTree()
	mode := "Folder view"
	if m.flatTree {
		mode = "Flat view"
	}
	return tea.Batch(m.treeSelect(m.ch.sel), m.toast(mode, "v switches between flat and folders", toastInfo))
}

func (m *Model) treeKey(key string) tea.Cmd {
	tr, _, _ := m.layout()
	if i, ok := listMove(key, m.ch.sel, m.itemCount(), tr.inner().h/2); ok {
		return m.treeSelect(i)
	}
	switch key {
	case "a":
		if len(m.status.Unstaged) > 0 {
			return m.runQuiet("Staged all", m.repo.StageAll)
		}
		return m.runQuiet("Unstaged all", m.repo.UnstageAll)
	case "v":
		return m.toggleTreeMode()
	}
	r, ok := m.selectedRow()
	if !ok {
		return nil
	}
	collapsed := r.kind == rowDir && m.collapsed[r.path]
	switch key {
	case "space", "s":
		name := r.path
		if r.kind == rowDir {
			name = fmt.Sprintf("%s/ (%s)", r.path, plural(len(r.changes), "file"))
		}
		paths := r.paths()
		if r.state == stageFull {
			return m.runQuiet("Unstaged "+name, func() error { return m.repo.Unstage(paths...) })
		}
		return m.runQuiet("Staged "+name, func() error { return m.repo.Stage(paths...) })
	case "enter":
		if r.kind == rowDir {
			return m.setCollapsed(r, !collapsed)
		}
		m.focus = boxDiff
	case "l", "right":
		switch {
		case r.kind == rowFile:
			m.focus = boxDiff
		case collapsed:
			return m.setCollapsed(r, false)
		default:
			return m.treeSelect(m.ch.sel + 1)
		}
	case "h", "left":
		if r.kind == rowDir && !collapsed {
			return m.setCollapsed(r, true)
		}
		if p := m.parentRow(); p >= 0 {
			return m.treeSelect(p)
		}
	case "e", "E":
		if r.kind != rowFile {
			return nil
		}
		if key == "e" {
			return m.openEditor(r.path)
		}
		return m.editFile(r.path)
	case "d", "x":
		files := r.unstaged()
		if len(files) == 0 {
			return m.toast("Unstage first", "discard only works on unstaged changes", toastInfo)
		}
		if r.kind == rowDir {
			return m.confirm("Discard folder", fmt.Sprintf("Discard every unstaged change in %s/ (%s)? Untracked files are deleted. This cannot be undone.", r.path, plural(len(files), "file")), func() tea.Cmd {
				return m.run("Discarded "+r.path+"/", func() error {
					for _, f := range files {
						if err := m.repo.Discard(f); err != nil {
							return err
						}
					}
					return nil
				})
			})
		}
		f := files[0]
		verb := "Discard unstaged changes to"
		if f.Untracked {
			verb = "Delete untracked file"
		}
		return m.confirm("Discard changes", fmt.Sprintf("%s %s? This cannot be undone.", verb, f.Path), func() tea.Cmd {
			return m.run("Discarded "+f.Path, func() error { return m.repo.Discard(f) })
		})
	}
	return nil
}

// ─── Diff box ───────────────────────────────────────────────────────────────

func (m *Model) diffBoxKey(key string) tea.Cmd {
	_, dr, _ := m.layout()
	h := dr.inner().h
	f, ok := m.selectedChange()
	switch key {
	case "esc", "h", "left":
		m.focus = boxTree
		return nil
	case "e":
		if ok {
			return m.openEditor(f.Path)
		}
	case "E":
		if ok {
			return m.editFile(f.Path)
		}
	case "space", "s", "enter":
		if r, ok := m.selectedRow(); ok && r.kind == rowDir {
			return m.treeKey("space")
		}
		return m.hunkStage()
	case "d", "x":
		return m.hunkDiscard()
	}
	return m.diffKey(&m.chDiff, key, h)
}

func (m *Model) hunkStage() tea.Cmd {
	f, ok := m.selectedChange()
	p := &m.chDiff
	if !ok || p.d == nil || len(p.d.Hunks) == 0 {
		return nil
	}
	if f.Untracked {
		return m.runQuiet("Staged "+f.Path, func() error { return m.repo.Stage(f.Path) })
	}
	patch := p.d.HunkPatch(p.hunk)
	if f.Staged {
		return m.runQuiet("Unstaged hunk", func() error { return m.repo.ApplyPatch(patch, true, true) })
	}
	return m.runQuiet("Staged hunk", func() error { return m.repo.ApplyPatch(patch, true, false) })
}

func (m *Model) hunkDiscard() tea.Cmd {
	f, ok := m.selectedChange()
	p := &m.chDiff
	if !ok || f.Staged || f.Untracked || p.d == nil || len(p.d.Hunks) == 0 {
		return nil
	}
	patch := p.d.HunkPatch(p.hunk)
	return m.confirm("Discard hunk", fmt.Sprintf("Throw away hunk %d of %s?", p.hunk+1, f.Path), func() tea.Cmd {
		return m.run("Discarded hunk", func() error { return m.repo.ApplyPatch(patch, false, true) })
	})
}

// ─── History graph ──────────────────────────────────────────────────────────

func (m *Model) graphSelect(i int) tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	m.sel = clamp(i, 0, len(m.commits)-1)
	m.followGraph(false)
	m.computeFocus()
	return m.kick()
}

// laneJump moves to the nearest commit on an adjacent lane.
func (m *Model) laneJump(dir int) tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	cur := m.rows[m.sel].NodeCol
	maxLane := 0
	for _, r := range m.rows {
		maxLane = max(maxLane, r.NodeCol)
	}
	for lane := cur + dir; lane >= 0 && lane <= maxLane; lane += dir {
		best, bestD := -1, 1<<30
		for i, r := range m.rows {
			if r.NodeCol == lane {
				if d := abs(i - m.sel); d < bestD {
					best, bestD = i, d
				}
			}
		}
		if best >= 0 {
			return m.graphSelect(best)
		}
	}
	return nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (m *Model) graphKey(key string) tea.Cmd {
	n := len(m.commits)
	switch key {
	// positions run oldest → newest; index 0 is newest
	case "l", "right":
		return m.graphSelect(m.sel - 1)
	case "h", "left":
		return m.graphSelect(m.sel + 1)
	case "L", "shift+right":
		return m.graphSelect(m.sel - 10)
	case "H", "shift+left":
		return m.graphSelect(m.sel + 10)
	case "end", "G", "$":
		return m.graphSelect(0)
	case "home", "g", "0":
		return m.graphSelect(n - 1)
	case "k", "up":
		return m.laneJump(-1)
	case "j", "down":
		return m.laneJump(1)
	case "enter":
		return m.openCommit()
	case "/":
		m.searching = true
		m.search.SetValue(m.query)
		return m.search.Focus()
	case "n":
		return m.nextMatch(1)
	case "N":
		return m.nextMatch(-1)
	case "esc":
		m.query = ""
		m.focusMode = false
		m.focusSet = nil
		return nil
	case "F":
		m.focusMode = !m.focusMode
		m.computeFocus()
		return nil
	case "@":
		for i, c := range m.commits {
			if c.IsHead {
				return m.graphSelect(i)
			}
		}
	}
	c := m.selectedCommit()
	if c.WIP || c.Hash == "" {
		return nil
	}
	switch key {
	case "space", "o":
		return m.checkoutCommit(c)
	case "b":
		return m.prompt("New branch", "Branch name (from "+c.Short+")", "feature/something", "", func(name string) tea.Cmd {
			return m.run("Created "+name, func() error { return m.repo.CreateBranch(name, c.Hash, true) })
		})
	case "t":
		return m.tagForm(c)
	case "C":
		return m.confirm("Cherry-pick", fmt.Sprintf("Apply %s “%s” onto %s?", c.Short, truncate(c.Subject, 40), m.currentBranch()), func() tea.Cmd {
			return m.run("Cherry-picked "+c.Short, func() error { return m.repo.CherryPick(c.Hash) })
		})
	case "R":
		return m.confirm("Revert", fmt.Sprintf("Create a commit that reverts %s?", c.Short), func() tea.Cmd {
			return m.run("Reverted "+c.Short, func() error { return m.repo.Revert(c.Hash) })
		})
	case "x":
		return m.resetForm(c)
	case "r":
		return m.confirm("Rebase", fmt.Sprintf("Rebase %s onto %s?", m.currentBranch(), c.Short), func() tea.Cmd {
			return m.run("Rebased onto "+c.Short, func() error { return m.repo.Rebase(c.Hash) })
		})
	case "m":
		if ref := firstBranch(c); ref != "" {
			return m.mergeConfirm(ref)
		}
		return m.mergeConfirm(c.Hash)
	case "I":
		return m.interactive("Interactive rebase", m.repo.Interactive("rebase", "-i", "--autostash", c.Hash+"^"))
	case "y":
		return tea.Batch(tea.SetClipboard(c.Hash), m.toast("Copied "+c.Short, c.Hash, toastInfo))
	}
	return nil
}

func firstBranch(c git.Commit) string {
	for _, k := range []git.RefKind{git.RefLocal, git.RefRemote} {
		for _, r := range c.Refs {
			if r.Kind == k {
				return r.Name
			}
		}
	}
	return ""
}

func (m *Model) checkoutCommit(c git.Commit) tea.Cmd {
	for _, r := range c.Refs {
		if r.Kind == git.RefLocal {
			name := r.Name
			return m.run("Switched to "+name, func() error { return m.repo.Checkout(name) })
		}
	}
	for _, r := range c.Refs {
		if r.Kind == git.RefRemote {
			name := r.Name
			return m.run("Checked out "+name, func() error { return m.repo.CheckoutRemote(name) })
		}
	}
	if c.IsHead {
		return nil
	}
	return m.confirm("Detached HEAD", fmt.Sprintf("No branch points at %s. Check it out detached?", c.Short), func() tea.Cmd {
		return m.run("Detached at "+c.Short, func() error { return m.repo.CheckoutDetached(c.Hash) })
	})
}

func (m *Model) nextMatch(dir int) tea.Cmd {
	ms := m.matches() // newest first
	if len(ms) == 0 {
		return nil
	}
	if dir > 0 { // older
		for _, i := range ms {
			if i > m.sel {
				return m.graphSelect(i)
			}
		}
		return m.graphSelect(ms[0])
	}
	for j := len(ms) - 1; j >= 0; j-- {
		if ms[j] < m.sel {
			return m.graphSelect(ms[j])
		}
	}
	return m.graphSelect(ms[len(ms)-1])
}

func (m *Model) updateSearch(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.searching = false
		m.query = ""
		m.search.Blur()
		return nil
	case "enter":
		m.searching = false
		m.search.Blur()
		return nil
	case "down", "ctrl+n":
		return m.nextMatch(1)
	case "up", "ctrl+p":
		return m.nextMatch(-1)
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(k)
	if m.search.Value() != m.query {
		m.query = m.search.Value()
		if ms := m.matches(); len(ms) > 0 {
			for _, i := range ms {
				if i >= m.sel {
					return tea.Batch(cmd, m.graphSelect(i))
				}
			}
			return tea.Batch(cmd, m.graphSelect(ms[0]))
		}
	}
	return cmd
}

func (m *Model) mergeConfirm(ref string) tea.Cmd {
	cur := m.currentBranch()
	if cur == "" {
		cur = "HEAD"
	}
	return m.confirm("Merge", fmt.Sprintf("Merge %s into %s?", ref, cur), func() tea.Cmd {
		return m.run("Merged "+ref, func() error { return m.repo.Merge(ref) })
	})
}

// ─── Forms ──────────────────────────────────────────────────────────────────

func (m *Model) tagForm(c git.Commit) tea.Cmd {
	name, msg := "", ""
	return m.openForm("Tag "+c.Short, 60, func() tea.Cmd {
		n := strings.TrimSpace(name)
		if n == "" {
			return nil
		}
		return m.run("Tagged "+n, func() error { return m.repo.Tag(n, c.Hash, strings.TrimSpace(msg)) })
	}, huh.NewGroup(
		huh.NewInput().Title("Tag name").Placeholder("v1.0.0").Value(&name).Validate(func(s string) error {
			if strings.TrimSpace(s) == "" || strings.ContainsAny(s, " ~^:?*[\\") {
				return fmt.Errorf("invalid tag name")
			}
			return nil
		}),
		huh.NewInput().Title("Message").Placeholder("leave empty for a lightweight tag").Value(&msg),
	))
}

func (m *Model) resetForm(c git.Commit) tea.Cmd {
	mode := "mixed"
	return m.openForm("Reset to "+c.Short, 64, func() tea.Cmd {
		run := func() tea.Cmd {
			return m.run("Reset ("+mode+") to "+c.Short, func() error { return m.repo.Reset(c.Hash, mode) })
		}
		if mode == "hard" {
			return m.confirm("Hard reset", "Discard ALL uncommitted changes and move "+m.currentBranch()+" to "+c.Short+"?", run)
		}
		return run()
	}, huh.NewGroup(huh.NewSelect[string]().Title("Move "+m.currentBranch()+" to “"+truncate(c.Subject, 36)+"”").Options(
		huh.NewOption("soft  · keep changes staged", "soft"),
		huh.NewOption("mixed · keep changes unstaged", "mixed"),
		huh.NewOption("hard  · discard everything", "hard"),
	).Value(&mode)))
}

func (m *Model) stashForm() tea.Cmd {
	if !m.status.Dirty() {
		return m.toast("Nothing to stash", "working tree clean", toastInfo)
	}
	msg := ""
	untracked := true
	return m.openForm("Stash changes", 60, func() tea.Cmd {
		return m.run("Stashed", func() error { return m.repo.StashPush(strings.TrimSpace(msg), untracked) })
	}, huh.NewGroup(
		huh.NewInput().Title("Message").Placeholder("optional").Value(&msg),
		huh.NewConfirm().Title("Include untracked files?").Value(&untracked).Affirmative("Yes").Negative("No"),
	))
}

func (m *Model) push(force bool) tea.Cmd {
	st := m.status
	if st.Behind > 0 && !force {
		return m.confirm("Branch is behind", fmt.Sprintf("%s is %d behind %s. Force push with lease?", st.Branch, st.Behind, st.Upstream), func() tea.Cmd {
			return m.run("Force-pushed", func() error { return m.repo.Push(true, st) })
		})
	}
	label := "Pushed"
	if force {
		label = "Force-pushed"
	}
	return m.run(label, func() error { return m.repo.Push(force, st) })
}

// ─── Palette commands ───────────────────────────────────────────────────────

func (m *Model) commands() []command {
	gk := func(k string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			m.focus = boxGraph
			return m.graphKey(k)
		}
	}
	focus := func(b box) func(*Model) tea.Cmd { return func(m *Model) tea.Cmd { m.focus = b; return nil } }
	cs := []command{
		{"Commit staged changes", "c", "git", func(m *Model) tea.Cmd { return m.commitForm(false) }},
		{"Amend last commit", "A", "git", func(m *Model) tea.Cmd { return m.commitForm(true) }},
		{"Stage all changes", "a", "git", func(m *Model) tea.Cmd { return m.runQuiet("Staged all", m.repo.StageAll) }},
		{"Unstage all changes", "", "git", func(m *Model) tea.Cmd { return m.runQuiet("Unstaged all", m.repo.UnstageAll) }},
		{"Commit in $EDITOR (this once)", "", "git", func(m *Model) tea.Cmd { return m.commitInEditor(false, "") }},
		{"Setting: commit with editor / message box", "", "settings", func(m *Model) tea.Cmd { return m.toggleCommitMode() }},
		{"Fetch all remotes", "f", "remote", func(m *Model) tea.Cmd { return m.run("Fetched", m.repo.Fetch) }},
		{"Pull", "p", "remote", func(m *Model) tea.Cmd { return m.run("Pulled", m.repo.Pull) }},
		{"Push", "P", "remote", func(m *Model) tea.Cmd { return m.push(false) }},
		{"Force push (with lease)", "", "remote", func(m *Model) tea.Cmd {
			return m.confirm("Force push", "Overwrite the remote branch (with lease)?", func() tea.Cmd { return m.push(true) })
		}},
		{"Branches…", "B", "branch", func(m *Model) tea.Cmd { return m.openBranches() }},
		{"New branch from HEAD", "", "branch", func(m *Model) tea.Cmd {
			return m.prompt("New branch", "Branch name", "feature/something", "", func(n string) tea.Cmd {
				return m.run("Created "+n, func() error { return m.repo.CreateBranch(n, "", true) })
			})
		}},
		{"Stashes…", "Z", "stash", func(m *Model) tea.Cmd { return m.openStashes() }},
		{"Stash changes", "S", "stash", func(m *Model) tea.Cmd { return m.stashForm() }},
		{"Focus working tree", "1", "view", focus(boxTree)},
		{"Focus diff / editor", "2", "view", focus(boxDiff)},
		{"Focus history", "3", "view", focus(boxGraph)},
		{"Open selected commit", "⏎", "graph", gk("enter")},
		{"Search history", "/", "graph", gk("/")},
		{"Jump to HEAD", "@", "graph", gk("@")},
		{"Jump to newest", "G", "graph", gk("G")},
		{"Jump to oldest", "g", "graph", gk("g")},
		{"Focus lineage of selected commit", "F", "graph", gk("F")},
		{"Checkout selected commit", "space", "graph", gk("o")},
		{"Branch from selected commit", "b", "graph", gk("b")},
		{"Tag selected commit", "t", "graph", gk("t")},
		{"Cherry-pick selected commit", "C", "graph", gk("C")},
		{"Revert selected commit", "R", "graph", gk("R")},
		{"Reset to selected commit", "x", "graph", gk("x")},
		{"Rebase onto selected commit", "r", "graph", gk("r")},
		{"Interactive rebase from selected", "I", "graph", gk("I")},
		{"Merge selected into current", "m", "graph", gk("m")},
		{"Copy commit hash", "y", "graph", gk("y")},
		{"Pick theme", "T", "ui", func(m *Model) tea.Cmd { return m.openThemes() }},
		{"Sync theme from " + theme.TerminalName(), "", "ui", func(m *Model) tea.Cmd {
			m.cfg.Theme = theme.TerminalName()
			_ = theme.SaveConfig(m.cfg)
			return tea.Batch(m.useTerminalTheme(), m.toast("Theme follows "+theme.TerminalName(), "colours update live when you change it", toastInfo))
		}},
		{"Refresh", "ctrl+r", "ui", func(m *Model) tea.Cmd { return m.loadAll() }},
		{"Quit", "q", "ui", func(m *Model) tea.Cmd { return m.whenQuiet(tea.Quit) }},
	}
	if st := m.status.State; st != "" {
		cs = append([]command{
			{"Continue " + st, "", "git", func(m *Model) tea.Cmd {
				return m.interactive("Continued "+st, m.repo.Interactive(m.repo.ContinueCmd(st)...))
			}},
			{"Abort " + st, "", "git", func(m *Model) tea.Cmd {
				return m.confirm("Abort", "Abort the current "+st+"?", func() tea.Cmd {
					return m.run("Aborted "+st, func() error { return m.repo.Abort(st) })
				})
			}},
		}, cs...)
	}
	for _, b := range m.branches {
		if b.Head {
			continue
		}
		b := b
		cs = append(cs, command{"Switch to " + b.Name, "", "checkout", func(m *Model) tea.Cmd {
			if b.Remote {
				return m.run("Checked out "+b.Name, func() error { return m.repo.CheckoutRemote(b.Name) })
			}
			return m.run("Switched to "+b.Name, func() error { return m.repo.Checkout(b.Name) })
		}})
	}
	for _, t := range m.themes {
		t := t
		cs = append(cs, command{"Theme: " + t.Name, "", "theme", func(m *Model) tea.Cmd {
			m.cfg.Theme = t.Name
			_ = theme.SaveConfig(m.cfg)
			if t.Name == theme.TerminalName() {
				return m.useTerminalTheme()
			}
			m.usePresetTheme(t)
			return m.toast("Theme · "+t.Name, "", toastInfo)
		}})
	}
	return cs
}

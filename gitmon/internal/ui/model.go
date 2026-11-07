// Package ui implements the gitmon terminal interface.
//
// Layout:
//
//	╭ Working tree ───────╮╭ Diff / Editor ───────────────╮
//	│ staged / unstaged   ││ hunk-aware diff, inline edit │
//	╰─────────────────────╯╰──────────────────────────────╯
//	╭ History ─────────────────────────────────────────────╮
//	│ ●──●──●──◆──●  horizontal commit graph, oldest → now │
//	╰──────────────────────────────────────────────────────╯
package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/karim-w/gitmon/internal/git"
	"github.com/karim-w/gitmon/internal/graph"
	"github.com/karim-w/gitmon/internal/theme"
)

const logLimit = 4000

type box int

const (
	boxTree box = iota
	boxDiff
	boxGraph
)

// ─── Messages ───────────────────────────────────────────────────────────────

type dataMsg struct {
	status   git.Status
	commits  []git.Commit
	branches []git.Branch
	stashes  []git.Stash
	fp       string
	err      error
}

type pollMsg struct{}

type polledMsg struct {
	status git.Status
	fp     string
}

type detailMsg struct {
	hash string
	d    *git.CommitDetail
	err  error
}

type diffMsg struct {
	pane *diffPane
	key  string
	d    *git.Diff
	err  error
}

type opMsg struct {
	label string
	err   error
	quiet bool
}

type toastExpireMsg struct{ id int }
type themeTimeoutMsg struct{}
type openCommitMsg struct{}

// ─── Model ──────────────────────────────────────────────────────────────────

type Model struct {
	repo   *git.Repo
	themes []theme.Theme
	cfg    theme.Config
	st     *Styles // active theme
	dim    *Styles // scrim / focus-mode variant
	rs     *Styles // styles for the current render pass

	// terminal (Rex) theme mirroring
	termMode   bool
	term       theme.TermColors
	awaitTheme bool

	w, h   int
	ready  bool
	loaded bool
	focus  box

	commits  []git.Commit
	rows     []graph.Row
	status   git.Status
	branches []git.Branch
	stashes  []git.Stash
	fp       string
	loadErr  error

	// working tree
	ch         scroller
	oscPending int
	oscSent    time.Time
	deferred   []tea.Cmd
	tree       []treeRow
	flatTree   bool
	collapsed  map[string]bool
	chDiff     diffPane

	// editor (inside the diff box)
	editing  bool
	editor   textarea.Model
	editPath string
	editOrig string

	// history graph
	sel       int // index into commits (0 = newest)
	gx        scroller
	laneOff   int
	searching bool
	search    textinput.Model
	query     string
	focusMode bool
	focusSet  map[int]bool

	// floating commit view
	details  map[string]*git.CommitDetail
	insFiles scroller
	insFocus int
	insDiff  diffPane

	modal  modal
	modalY spring

	toasts   []*toast
	toastSeq int

	busy      int
	busyLabel string
	spin      spinner.Model
	animating bool
}

func New(repo *git.Repo, themeName string) *Model {
	cfg := theme.LoadConfig()
	all := theme.All()
	if theme.InRex() || cfg.Theme == "terminal" {
		all = append([]theme.Theme{theme.FromTerminal(theme.TermColors{})}, all...)
	}
	if themeName == "" {
		themeName = cfg.Theme
		if themeName == "" && theme.InRex() {
			themeName = "rex"
		}
	}
	m := &Model{
		repo:      repo,
		themes:    all,
		cfg:       cfg,
		ch:        newScroller(),
		flatTree:  cfg.FlatTree,
		collapsed: map[string]bool{},
		gx:        newScroller(),
		insFiles:  newScroller(),
		details:   map[string]*git.CommitDetail{},
		modalY:    newSpring(&popSpring, 0),
		focus:     boxTree,
	}
	m.chDiff = newDiffPane()
	m.insDiff = newDiffPane()
	m.search = textinput.New()
	m.search.Prompt = "/ "
	m.search.Placeholder = "search message, author, hash, branch…"
	m.editor = textarea.New()

	if themeName == "rex" || themeName == "terminal" {
		m.termMode = true
		m.awaitTheme = true
		m.applyTheme(theme.FromTerminal(theme.TermColors{}))
	} else {
		t, _ := theme.Find(all, themeName)
		m.applyTheme(t)
	}
	return m
}

func (m *Model) applyTheme(t theme.Theme) {
	m.st = newStyles(t)
	m.dim = newStyles(t.Dimmed(0.62))
	m.rs = m.st
	m.spin = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(fg(m.st.Accent2)))
	ss := textinput.DefaultStyles(t.Dark)
	ss.Focused.Prompt = fg(m.st.Accent2).Bold(true)
	ss.Focused.Text = fg(m.st.Text)
	ss.Focused.Placeholder = fg(m.st.Muted)
	ss.Blurred = ss.Focused
	ss.Cursor.Color = m.st.Accent2
	m.search.SetStyles(ss)
	m.styleEditor()
}

// useTerminalTheme switches to mirroring the host terminal's colours.
func (m *Model) useTerminalTheme() tea.Cmd {
	m.termMode = true
	m.applyTheme(theme.FromTerminal(m.term))
	return m.queryTerm()
}

func (m *Model) usePresetTheme(t theme.Theme) {
	if t.Name == theme.TerminalName() {
		m.termMode = true
		m.applyTheme(theme.FromTerminal(m.term))
		return
	}
	m.termMode = false
	m.applyTheme(t)
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadAll(), poll()}
	if m.termMode {
		cmds = append(cmds, m.queryTerm(),
			tea.Tick(600*time.Millisecond, func(time.Time) tea.Msg { return themeTimeoutMsg{} }))
	}
	return tea.Batch(cmds...)
}

// ─── Commands ───────────────────────────────────────────────────────────────

func (m *Model) loadAll() tea.Cmd {
	r := m.repo
	return func() tea.Msg {
		var d dataMsg
		d.status, d.err = r.Status()
		if d.err != nil {
			return d
		}
		d.commits, d.err = r.Log(logLimit)
		if d.err != nil {
			return d
		}
		d.branches, _ = r.Branches()
		d.stashes, _ = r.Stashes()
		d.fp = r.Fingerprint()
		return d
	}
}

func poll() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return pollMsg{} })
}

func (m *Model) doPoll() tea.Cmd {
	r := m.repo
	return func() tea.Msg {
		st, err := r.Status()
		if err != nil {
			return nil
		}
		return polledMsg{status: st, fp: r.Fingerprint()}
	}
}

func (m *Model) fetchDetail(hash string) tea.Cmd {
	if hash == "" || m.details[hash] != nil {
		return nil
	}
	r := m.repo
	return func() tea.Msg {
		d, err := r.Detail(hash)
		return detailMsg{hash: hash, d: d, err: err}
	}
}

func (m *Model) run(label string, fn func() error) tea.Cmd {
	m.busy++
	m.busyLabel = label
	return tea.Batch(m.spin.Tick, func() tea.Msg { return opMsg{label: label, err: fn()} })
}

func (m *Model) runQuiet(label string, fn func() error) tea.Cmd {
	m.busy++
	m.busyLabel = label
	return tea.Batch(m.spin.Tick, func() tea.Msg { return opMsg{label: label, err: fn(), quiet: true} })
}

func (m *Model) interactive(label string, c *exec.Cmd) tea.Cmd {
	return m.whenQuiet(tea.ExecProcess(c, func(err error) tea.Msg { return opMsg{label: label, err: err} }))
}

func (m *Model) kick() tea.Cmd {
	if m.animating {
		return nil
	}
	m.animating = true
	return frame()
}

// ─── Update ─────────────────────────────────────────────────────────────────

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.ready = true
		m.layoutEditor()
		m.followGraph(true)
		if m.modal != nil {
			return m, m.updateModal(msg)
		}
		return m, nil

	case frameMsg:
		return m, m.stepAnimations()

	case spinner.TickMsg:
		if m.busy == 0 {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	// ── terminal theme replies ──
	case tea.BackgroundColorMsg:
		m.onTermColor(func() { m.term.Bg = msg.Color })
		return m, m.gotOSCReply()
	case tea.ForegroundColorMsg:
		m.onTermColor(func() { m.term.Fg = msg.Color })
		return m, m.gotOSCReply()
	case uv.UnknownOscEvent:
		if i, c, ok := theme.ParseOSC4(string(msg)); ok {
			m.onTermColor(func() { m.term.ANSI[i] = c })
			return m, m.gotOSCReply()
		}
		return m, nil
	case flushDeferredMsg:
		return m, m.flushDeferred()
	case themeTimeoutMsg:
		m.awaitTheme = false
		return m, nil

	case dataMsg:
		return m, m.onData(msg)

	case pollMsg:
		cmds := []tea.Cmd{m.doPoll(), poll()}
		if m.termMode {
			cmds = append(cmds, m.queryTerm())
		}
		return m, tea.Batch(cmds...)

	case polledMsg:
		if msg.fp != m.fp || !reflect.DeepEqual(msg.status, m.status) {
			return m, m.loadAll()
		}
		return m, nil

	case tea.FocusMsg:
		cmds := []tea.Cmd{m.loadAll()}
		if m.termMode {
			cmds = append(cmds, m.queryTerm())
		}
		return m, tea.Batch(cmds...)

	case detailMsg:
		if msg.err == nil {
			m.details[msg.hash] = msg.d
			if _, ok := m.modal.(*commitModal); ok && m.selectedCommit().Hash == msg.hash {
				return m, m.loadInspectDiff()
			}
		}
		return m, nil

	case diffMsg:
		if msg.pane.key == msg.key {
			msg.pane.setDiff(msg.d, msg.err)
		}
		return m, nil

	case opMsg:
		m.busy = max(0, m.busy-1)
		var cmds []tea.Cmd
		if msg.err != nil {
			cmds = append(cmds, m.toast(msg.label+" failed", msg.err.Error(), toastError))
		} else if !msg.quiet {
			cmds = append(cmds, m.toast(msg.label, "", toastSuccess))
		}
		m.details = map[string]*git.CommitDetail{}
		cmds = append(cmds, m.loadAll())
		return m, tea.Batch(cmds...)

	case openCommitMsg:
		if st, err := m.repo.Status(); err == nil {
			m.status = st
		}
		return m, tea.Batch(m.commitForm(false), m.loadAll())

	case toastExpireMsg:
		for _, t := range m.toasts {
			if t.id == msg.id {
				t.leaving = true
				t.x.set(float64(toastWidth + 4))
			}
		}
		return m, m.kick()

	case tea.MouseWheelMsg:
		if m.modal != nil {
			return m, m.updateModal(msg)
		}
		return m, m.onWheel(msg)

	case tea.MouseClickMsg:
		if m.modal != nil {
			return m, nil
		}
		return m, m.onClick(msg)

	case tea.KeyPressMsg:
		if m.modal != nil {
			return m, m.updateModal(msg)
		}
		if m.searching {
			return m, m.updateSearch(msg)
		}
		if m.editing && m.focus == boxDiff {
			return m, m.editorKey(msg)
		}
		return m, m.onKey(msg)
	}

	if m.modal != nil {
		return m, m.updateModal(msg)
	}
	if m.searching {
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	}
	if m.editing {
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) onTermColor(set func()) {
	before := m.term
	set()
	if m.term.Bg != nil {
		m.awaitTheme = false
	}
	if m.termMode && !reflect.DeepEqual(before, m.term) {
		t := theme.FromTerminal(m.term)
		m.applyTheme(t)
		if len(m.themes) > 0 && m.themes[0].Name == t.Name {
			m.themes[0] = t
		}
	}
}

func (m *Model) updateModal(msg tea.Msg) tea.Cmd {
	md := m.modal
	closed, cmd := md.Update(m, msg)
	if closed && m.modal == md {
		m.modal = nil
	}
	return cmd
}

func (m *Model) stepAnimations() tea.Cmd {
	moving := false
	for _, s := range []*spring{&m.ch.spring, &m.gx.spring, &m.insFiles.spring,
		&m.insDiff.off, &m.chDiff.off, &m.modalY} {
		if s.step() {
			moving = true
		}
	}
	if lm, ok := m.modal.(*listModal); ok && lm.sc.step() {
		moving = true
	}
	if p, ok := m.modal.(*paletteModal); ok && p.sc.step() {
		moving = true
	}
	alive := m.toasts[:0]
	for _, t := range m.toasts {
		if t.x.step() {
			moving = true
		}
		if !(t.leaving && t.x.settled()) {
			alive = append(alive, t)
		}
	}
	m.toasts = alive
	if moving {
		return frame()
	}
	m.animating = false
	return nil
}

func (m *Model) onData(d dataMsg) tea.Cmd {
	if d.err != nil {
		m.loadErr = d.err
		return nil
	}
	first := !m.loaded
	m.loadErr = nil
	m.loaded = true
	prevSel := m.selectedCommit()
	m.status, m.branches, m.stashes, m.fp = d.status, d.branches, d.stashes, d.fp
	m.rebuildTree()
	commits := d.commits
	if d.status.Dirty() && d.status.Head != "" {
		wip := git.Commit{WIP: true, Parents: []string{d.status.Head}, Subject: "Uncommitted changes", Time: time.Now()}
		commits = append([]git.Commit{wip}, commits...)
	}
	m.commits = commits
	ids := make([]string, len(commits))
	parents := make([][]string, len(commits))
	for i, c := range commits {
		ids[i] = c.Hash
		if c.WIP {
			ids[i] = "\x00wip"
		}
		parents[i] = c.Parents
	}
	m.rows = graph.Build(ids, parents)

	switch {
	case first:
		m.sel = 0
		if len(commits) > 0 && !commits[0].WIP {
			for i, c := range commits {
				if c.IsHead {
					m.sel = i
					break
				}
			}
		}
	case prevSel.WIP && len(commits) > 0 && commits[0].WIP:
		m.sel = 0
	case prevSel.Hash != "":
		m.sel = clamp(m.sel, 0, max(0, len(commits)-1))
		for i, c := range commits {
			if c.Hash == prevSel.Hash {
				m.sel = i
				break
			}
		}
	default:
		m.sel = clamp(m.sel, 0, max(0, len(commits)-1))
	}
	m.followGraph(first)
	m.computeFocus()
	if cm, ok := m.modal.(*commitModal); ok && m.selectedCommit().WIP {
		_ = cm
		m.modal = nil
	}
	return tea.Batch(m.loadChangeDiff(true), m.kick())
}

// ─── Selection helpers ──────────────────────────────────────────────────────

func (m *Model) selectedCommit() git.Commit {
	if m.sel >= 0 && m.sel < len(m.commits) {
		return m.commits[m.sel]
	}
	return git.Commit{}
}

func (m *Model) changeCount() int { return len(m.status.Staged) + len(m.status.Unstaged) }

func (m *Model) currentBranch() string {
	if m.status.Detached {
		return ""
	}
	return m.status.Branch
}

// ─── Loaders ────────────────────────────────────────────────────────────────

func (m *Model) loadChangeDiff(force bool) tea.Cmd {
	row, ok := m.selectedRow()
	if !ok {
		m.chDiff.clear()
		return nil
	}
	key := row.key()
	if !force && m.chDiff.key == key {
		return nil
	}
	keep := m.chDiff.key == key
	r := m.repo
	if row.kind == rowDir {
		st, un := row.staged(), row.unstaged()
		return m.chDiff.load(key, keep, func() (string, error) { return r.ChangesDiff(st, un) })
	}
	f := row.file()
	return m.chDiff.load(key, keep, func() (string, error) { return r.WorkingDiff(f) })
}

func (m *Model) loadInspectDiff() tea.Cmd {
	c := m.selectedCommit()
	d := m.details[c.Hash]
	if d == nil {
		return m.fetchDetail(c.Hash)
	}
	if len(d.Files) == 0 {
		m.insDiff.clear()
		return nil
	}
	f := d.Files[clamp(m.insFiles.sel, 0, len(d.Files)-1)]
	key := c.Hash + ":" + f.Path
	if m.insDiff.key == key {
		return nil
	}
	r := m.repo
	return m.insDiff.load(key, false, func() (string, error) { return r.CommitFileDiff(d, f.Path) })
}

func (m *Model) editFile(path string) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], filepath.Join(m.repo.Root, path))...)
	return m.interactive("Edited "+filepath.Base(path), c)
}

// computeFocus marks every commit that is an ancestor or descendant of the
// selection, so focus mode can fade unrelated history.
func (m *Model) computeFocus() {
	if !m.focusMode || len(m.commits) == 0 {
		m.focusSet = nil
		return
	}
	idx := make(map[string]int, len(m.commits))
	for i, c := range m.commits {
		if !c.WIP {
			idx[c.Hash] = i
		}
	}
	children := make([][]int, len(m.commits))
	for i, c := range m.commits {
		for _, p := range c.Parents {
			if j, ok := idx[p]; ok {
				children[j] = append(children[j], i)
			}
		}
	}
	set := map[int]bool{m.sel: true}
	stack := []int{m.sel}
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, p := range m.commits[i].Parents {
			if j, ok := idx[p]; ok && !set[j] {
				set[j] = true
				stack = append(stack, j)
			}
		}
	}
	stack = []int{m.sel}
	seen := map[int]bool{m.sel: true}
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, c := range children[i] {
			if !seen[c] {
				seen[c], set[c] = true, true
				stack = append(stack, c)
			}
		}
	}
	m.focusSet = set
}

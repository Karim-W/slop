package ui

import (
	"fmt"
	"hash/fnv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"rsc.io/qr"

	"github.com/karim-w/slop/apps/kyro/internal/engine"
	"github.com/karim-w/slop/apps/kyro/internal/home"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/apps/kyro/internal/sound"
	"github.com/karim-w/slop/apps/kyro/internal/web"
	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/utils/humanize"
)

// The web player mirrors this model: after every message the state goes
// to the open pages (the server sends only what changed).

func (m *Model) publish() {
	if m.d.Web == nil || !m.d.Web.Running() {
		return
	}
	m.d.Web.Publish(m.webState(), func() []library.Track { return m.pl.State().Queue })
}

func (m *Model) webState() web.State {
	s := m.pl.State()
	st := web.State{
		Active: s.Active, Paused: s.Paused, Pos: s.Pos.Seconds(), Duration: s.Duration.Seconds(),
		Volume: s.Volume, Shuffle: s.Shuffle, Repeat: string(s.Repeat), Sound: sound.Find(m.cfg.Audio.Sound).Name,
		Index: s.Index, QueueLen: len(s.Queue), QueueRev: queueRev(s.Queue),
		Theme: m.base.T, Dynamic: m.cfg.DynamicTheme, LibraryRev: m.libRev, PlaylistsRev: m.plRev,
	}
	if key, ok := strings.CutPrefix(m.themeTarget, "cover:"); ok && m.covers[key] != nil {
		st.Theme = *m.covers[key]
	}
	if s.Active {
		t := s.Track
		if s.Duration > 0 {
			t.Duration = s.Duration
		}
		wt := web.TrackOf(t, m.d.Ratings.Get(t.Path))
		st.Track = &wt
		rate, ch := t.SampleRate, t.Channels
		if s.Source.SampleRate > 0 && m.onLocal() {
			rate, ch = s.Source.SampleRate, s.Source.Channels
		}
		st.Quality = library.Quality(t.Codec, rate, t.BitDepth, ch)
	}
	if m.d.Router != nil {
		st.Outputs, st.Output = m.d.Router.Outputs(), m.d.Router.Active()
	}
	return st
}

// queueRev changes whenever the queue's songs or their order do.
func queueRev(q []library.Track) string {
	h := fnv.New64a()
	for _, t := range q {
		h.Write([]byte(t.ID))
	}
	return fmt.Sprintf("%x-%d", h.Sum64(), len(q))
}

// tracksByID resolves IDs from a page: library tracks, or queue entries
// (songs from playlists that aren't in the library).
func (m *Model) tracksByID(ids []string) []library.Track {
	queue := map[string]library.Track{}
	for _, t := range m.pl.State().Queue {
		queue[t.ID] = t
	}
	out := make([]library.Track, 0, len(ids))
	for _, id := range ids {
		if t, ok := m.lib.Get(id); ok {
			out = append(out, t)
		} else if t, ok := queue[id]; ok {
			out = append(out, t)
		}
	}
	return out
}

// onLocal reports whether this computer is the output.
func (m *Model) onLocal() bool { return m.d.Router == nil || m.d.Router.Active() == "local" }

// outputName is where the song plays when that isn't this computer.
func (m *Model) outputName() string {
	if m.onLocal() {
		return ""
	}
	active := m.d.Router.Active()
	for _, o := range m.d.Router.Outputs() {
		if o.ID == active {
			return o.Name
		}
	}
	return ""
}

// ─── Outputs ────────────────────────────────────────────────────────────────

// outputPicker moves the song between this computer and browser speakers.
type outputPicker struct {
	sel    int
	scroll tui.Scroll
}

func (m *Model) outputs() []engine.Output {
	if m.d.Router == nil {
		return nil
	}
	return m.d.Router.Outputs()
}

func newOutputPicker(m *Model) *outputPicker {
	p := &outputPicker{}
	for i, o := range m.outputs() {
		if o.Active {
			p.sel = i
		}
	}
	return p
}

func (p *outputPicker) Update(m *Model, msg tea.Msg) (modal, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}
	outs := m.outputs()
	switch k.String() {
	case "esc", "q", "o":
		return nil, nil
	case "up", "k":
		p.sel = max(p.sel-1, 0)
	case "down", "j":
		p.sel = min(p.sel+1, max(len(outs)-1, 0))
	case "w":
		return newWebDialog(m), nil
	case "enter":
		if p.sel >= len(outs) {
			return nil, nil
		}
		if err := m.d.Router.Switch(outs[p.sel].ID); err != nil {
			return nil, m.fail(err)
		}
		return nil, m.showFlash("Playing on "+outs[p.sel].Name, true)
	}
	return p, nil
}

func (p *outputPicker) View(m *Model) tui.View {
	outs := m.outputs()
	list := tui.List(len(outs), p.sel, &p.scroll, func(i int, sel bool) tui.View {
		o := outs[i]
		bar, icon := tui.Text("  "), "◌ "
		if sel {
			bar = tui.Text("▌ ").Fg(tui.Accent2)
		}
		if o.Active {
			icon = "◉ "
		}
		kind := "this computer"
		if o.Kind == "browser" {
			kind = "browser"
		}
		return tui.HStack(bar, tui.Text(icon).Fg(tui.Accent), tui.Width(28, tui.Text(o.Name).Bold()), tui.Text(kind).Fg(tui.Muted))
	}).Highlight(tui.Selection)
	note := "open the web player on a phone or TV and press Play here to add it"
	if m.d.Web == nil || !m.d.Web.Running() {
		note = "turn on the web player (w) to play on a phone, a TV or another computer"
	}
	return tui.Dialog("Play on", tui.Height(max(len(outs), 1), list), tui.Text(""), tui.Text(note).Fg(tui.Muted)).
		Width(min(70, m.w-2)).Hints(tui.H("↑↓", "move"), tui.H("⏎", "play there"), tui.H("w", "web player"), tui.H("esc", "close"))
}

// ─── Web player ─────────────────────────────────────────────────────────────

// webDialog turns the web player on and off, pairs browsers and lists
// the paired ones, any of which can be unpaired. Pairing is only possible
// while it is open, like a Bluetooth pairing window.
type webDialog struct {
	sel     int
	scroll  tui.Scroll
	confirm string // "one" or "all" while asking
}

func newWebDialog(m *Model) *webDialog {
	if m.d.Web != nil && m.d.Web.Running() {
		m.d.Web.OpenPairing()
	}
	return &webDialog{}
}

func (d *webDialog) close(m *Model) {
	if m.d.Web != nil {
		m.d.Web.ClosePairing()
	}
}

func (d *webDialog) Update(m *Model, msg tea.Msg) (modal, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	if m.d.Web == nil {
		return nil, nil
	}
	s := m.d.Web
	paired := s.Paired()
	if d.confirm != "" {
		which := d.confirm
		d.confirm = ""
		if k.String() != "y" {
			return d, nil
		}
		id, what := "", "Every browser has to pair again"
		if which == "one" && d.sel < len(paired) {
			id, what = paired[d.sel].ID, "Unpaired "+paired[d.sel].Name
		}
		if err := s.Revoke(id); err != nil {
			return d, m.fail(err)
		}
		d.sel = 0
		return d, m.showFlash(what, true)
	}
	switch k.String() {
	case "esc", "q", "w":
		d.close(m)
		return nil, nil
	case "up", "k":
		d.sel = max(d.sel-1, 0)
	case "down", "j":
		d.sel = min(d.sel+1, max(len(paired)-1, 0))
	case "enter":
		on := !s.Running()
		if on {
			if err := s.Start(m.cfg.Web.Port); err != nil {
				return d, m.fail(err)
			}
			s.OpenPairing()
		} else {
			s.Stop()
		}
		m.cfg.Web.Enabled = on
		if err := home.Save(m.cfg); err != nil {
			return d, m.fail(err)
		}
		return d, m.showFlash(map[bool]string{true: "Web player on", false: "Web player off"}[on], true)
	case "p":
		if s.Running() {
			s.OpenPairing()
		}
	case "x", "delete", "backspace":
		if len(paired) > 0 {
			d.confirm = "one"
		}
	case "X":
		if len(paired) > 0 {
			d.confirm = "all"
		}
	case "o":
		d.close(m)
		return newOutputPicker(m), nil
	}
	return d, nil
}

func (d *webDialog) View(m *Model) tui.View {
	if m.d.Web == nil {
		return tui.Dialog("Web player", tui.Text("The web player isn't available in this build.").Fg(tui.Muted))
	}
	s := m.d.Web
	width := min(88, m.w-2)
	if !s.Running() {
		port := m.cfg.Web.Port
		if port == 0 {
			port = web.Port
		}
		return tui.Dialog("Web player",
			tui.Line(tui.S("○ ", tui.Fg(tui.Muted)), tui.S("Off", tui.Fg(tui.Primary).Bold())),
			tui.Text(""),
			tui.Text("Turn it on to use kyro from a browser on this network:").Fg(tui.Secondary),
			tui.Text("  · a TV, to take the song over on bigger speakers"),
			tui.Text("  · a phone, as a remote for the Mac or the TV"),
			tui.Text("  · a phone, as the speaker (its Bluetooth speaker too)"),
			tui.Text(""),
			tui.Textf("kyro listens on port %d. Only browsers you pair get in.", port).Fg(tui.Muted),
		).Width(width).Hints(tui.H("⏎", "turn on"), tui.H("esc", "close"))
	}
	urls := s.URLs()
	code := s.PairingCode()
	info := []tui.View{
		tui.Line(tui.S("● ", tui.Fg(tui.Success)), tui.S("On", tui.Fg(tui.Primary).Bold())), tui.Text(""),
		tui.Text("Open on your phone or TV").Fg(tui.Secondary),
	}
	for _, u := range urls {
		info = append(info, tui.Text(u).Fg(tui.Accent).Bold())
	}
	info = append(info, tui.Text(""))
	if code != "" {
		info = append(info, tui.Line(tui.S("Pairing code  ", tui.Fg(tui.Secondary)), tui.S(code[:3]+" "+code[3:], tui.Fg(tui.Accent2).Bold())),
			tui.Text("Pair once: the browser stays paired.").Fg(tui.Muted))
	} else {
		info = append(info, tui.Text("Pairing closed after wrong codes · p for a new code").Fg(tui.Warning))
	}
	top := tui.View(tui.VStack(info...))
	if code != "" && len(urls) > 0 && m.w >= 80 && m.h >= 30 {
		if q := qrView(urls[0] + "/#code=" + code); q != nil {
			top = tui.HStack(q, tui.Text("   "), top)
		}
	}

	paired := s.Paired()
	d.sel = min(d.sel, max(len(paired)-1, 0))
	var list tui.View = tui.Text("No browser is paired yet.").Fg(tui.Muted)
	if len(paired) > 0 {
		list = tui.Height(min(len(paired), 6), tui.List(len(paired), d.sel, &d.scroll, func(i int, sel bool) tui.View {
			p := paired[i]
			bar, dot := tui.Text("  "), tui.Text("○ ").Fg(tui.Muted)
			if sel {
				bar = tui.Text("▌ ").Fg(tui.Accent2)
			}
			seen := "seen " + humanize.Ago(p.Seen)
			if p.Live {
				dot, seen = tui.Text("● ").Fg(tui.Success), "open now"
			}
			return tui.HStack(bar, dot, tui.Width(26, tui.Text(p.Name).Bold()),
				tui.Width(16, tui.Text(seen).Fg(tui.Secondary)), tui.Text("paired "+p.Created.Format("2 Jan 2006")).Fg(tui.Muted))
		}).Highlight(tui.Selection))
	}
	body := []tui.View{top, tui.Text(""), tui.Textf("Paired browsers (%d)", len(paired)).Fg(tui.Accent).Bold(), list}
	switch {
	case d.confirm == "one" && d.sel < len(paired):
		body = append(body, tui.Text(""), tui.Textf("Unpair %s? It is cut off now and needs a new code. y / n", paired[d.sel].Name).Fg(tui.Danger).Bold())
	case d.confirm == "all":
		body = append(body, tui.Text(""), tui.Text("Unpair every browser? y / n").Fg(tui.Danger).Bold())
	}
	return tui.Dialog("Web player", body...).Width(width).
		Hints(tui.H("↑↓", "browser"), tui.H("x", "unpair"), tui.H("X", "unpair all"), tui.H("p", "new code"), tui.H("o", "play on"), tui.H("⏎", "turn off"), tui.H("esc", "close"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// qrView draws a QR code with half blocks, two modules per cell, always
// dark on light so cameras read it whatever the theme.
func qrView(text string) tui.View {
	c, err := qr.Encode(text, qr.L)
	if err != nil {
		return nil
	}
	const quiet = 2
	n := c.Size + 2*quiet
	dark := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		return x >= 0 && y >= 0 && x < c.Size && y < c.Size && c.Black(x, y)
	}
	black, white := lipgloss.Color("#000000"), lipgloss.Color("#ffffff")
	var lines []string
	for y := 0; y < n; y += 2 {
		var b strings.Builder
		for x := 0; x < n; x++ {
			fg, bg := white, white
			if dark(x, y) {
				fg = black
			}
			if y+1 < n && dark(x, y+1) {
				bg = black
			}
			b.WriteString(lipgloss.NewStyle().Foreground(fg).Background(bg).Render("▀"))
		}
		lines = append(lines, b.String())
	}
	return tui.Frame(n, len(lines), tui.Func(func(tui.Ctx, tui.Size) []string { return lines }))
}

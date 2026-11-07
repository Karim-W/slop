package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/karim-w/slop/apps/kyro/internal/art"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/apps/kyro/internal/player"
	"github.com/karim-w/slop/apps/kyro/internal/sound"
	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/utils/textx"
)

type layoutKind int

const (
	layoutMini    layoutKind = iota // a sliver: one or two lines
	layoutCompact                   // a tiling pane: now-playing card + list
	layoutFull                      // a window: sidebar, table, now-playing bar
)

func (m *Model) layout() layoutKind {
	switch {
	case m.h < 9 || m.w < 40:
		return layoutMini
	case m.w < 96 || m.h < 24:
		return layoutCompact
	}
	return layoutFull
}

func (m *Model) View() tea.View {
	v := tea.NewView(tui.Render(m.pal, m.root(), m.w, m.h))
	v.AltScreen = true
	v.ReportFocus = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "kyro"
	if s := m.pl.State(); s.Active {
		v.WindowTitle = "kyro · " + s.Track.Title
	}
	if !m.termMode || m.coverOn {
		v.BackgroundColor = m.pal.Bg
		v.ForegroundColor = m.pal.Text
	}
	return v
}

func (m *Model) root() tui.View {
	if m.w <= 0 || m.h <= 0 {
		return nil
	}
	var screen tui.View
	switch {
	case m.zen && m.h >= 9 && m.w >= 30:
		screen = m.zenView()
	case m.layout() == layoutMini:
		screen = m.miniView()
	case m.layout() == layoutCompact:
		screen = m.compactView()
	default:
		screen = m.fullView()
	}
	var dialog tui.View
	if m.modal != nil {
		dialog = m.modal.View(m)
	}
	return tui.Modal(screen, dialog)
}

// ─── Full ───────────────────────────────────────────────────────────────────

func (m *Model) fullView() tui.View {
	return tui.VStack(
		m.header(),
		tui.HStack(
			tui.Width(28, m.sidebar()),
			m.listPanel(),
		).Grow(),
		tui.Height(7, m.nowPlayingBar()),
		m.footer(),
	)
}

func (m *Model) header() tui.View {
	right := tui.Text(m.scan).Fg(tui.Muted)
	if m.scan == "" {
		right = tui.Textf("%d tracks", len(m.tracks)).Fg(tui.Muted)
	}
	return tui.HStack(
		tui.Gradient(" ◖ kyro ◗ ").Bold(),
		tui.Text(" "),
		tui.Text(m.breadcrumb()).Fg(tui.Secondary),
		tui.Spacer(),
		right,
		tui.Text(" "),
	)
}

func (m *Model) breadcrumb() string {
	name := sectionNames[m.sec]
	if m.sec == secPlaylist {
		name = "♫ " + m.playlist
	}
	if m.drill != nil {
		name += " › " + m.drill.group
	}
	return name
}

func (m *Model) sidebar() tui.View {
	items := m.sideItems()
	focused := m.focus == focusSidebar
	counts := map[section]int{secSongs: len(m.tracks), secQueue: len(m.pl.State().Queue)}
	counts[secAlbums] = len(m.groups(func(t library.Track) (string, string) { return t.Album, t.SortArtist() }, ""))
	counts[secArtists] = len(m.groups(func(t library.Track) (string, string) { return t.SortArtist(), "" }, ""))
	icons := map[section]string{secSongs: "♪", secAlbums: "◎", secArtists: "☺", secQueue: "≡", secLoved: "♥"}
	counts[secLoved] = 0
	loved := m.d.Ratings.Loved()
	for _, t := range m.tracks {
		if loved[t.Path] {
			counts[secLoved]++
		}
	}
	plCount := map[string]int{}
	for _, p := range m.playlists {
		plCount[p.Name] = len(p.Entries)
	}

	var rows []tui.View
	heading := func(s string) { rows = append(rows, tui.Text(" "+s).Fg(tui.Muted).Bold()) }
	heading("LIBRARY")
	for i, it := range items {
		if i == 5 {
			rows = append(rows, tui.Text(""))
			heading("PLAYLISTS")
		}
		active := !it.create && it.sec == m.sec && it.playlist == m.playlist
		sel := focused && i == m.side
		var label, icon, count string
		switch {
		case it.create:
			icon, label = "+", "New playlist"
		case it.sec == secPlaylist:
			icon, label, count = "♫", it.playlist, fmt.Sprint(plCount[it.playlist])
		default:
			icon, label, count = icons[it.sec], sectionNames[it.sec], fmt.Sprint(counts[it.sec])
		}
		bar := tui.Text("  ")
		lc := tui.Color(tui.Secondary)
		switch {
		case sel:
			bar, lc = tui.Text("▌ ").Fg(tui.Accent2), tui.Primary
		case active:
			bar, lc = tui.Text("▎ ").Fg(tui.Accent), tui.Accent
		}
		if it.create {
			lc = tui.Muted
		}
		line := tui.HStack(bar, tui.Text(icon+" ").Fg(tui.Accent), tui.Text(label).Fg(lc).Grow(), tui.Text(count+" ").Fg(tui.Muted))
		if sel {
			rows = append(rows, tui.Tint(tui.Selection, line))
		} else {
			rows = append(rows, line)
		}
	}
	return tui.Panel(tui.VStack(rows...)).Title("Browse").Focused(focused)
}

// ─── Track list ─────────────────────────────────────────────────────────────

func (m *Model) listPanel() tui.PanelView {
	focused := m.focus == focusList
	label := tui.View(tui.Textf("%d", len(m.rows)).Fg(tui.Muted))
	if m.filter.Value() != "" || m.filtering {
		label = tui.Text("filter: " + m.filter.Value()).Fg(tui.Warning)
	}
	hints := []tui.Hint{tui.H("⏎", "play"), tui.H("a", "add to playlist"), tui.H("e", "queue"), tui.H("/", "filter")}
	if m.sec == secPlaylist {
		hints = []tui.Hint{tui.H("⏎", "play"), tui.H("x", "remove"), tui.H("J/K", "move"), tui.H("R", "rename"), tui.H("X", "export")}
	}
	if m.drill != nil || m.sec == secAlbums || m.sec == secArtists {
		hints = append([]tui.Hint{tui.H("⏎", "open")}, hints[1:]...)
		if m.drill != nil {
			hints = append(hints, tui.H("esc", "back"))
		}
	}
	return tui.Panel(m.list()).Title(m.breadcrumb()).Label(label).Focused(focused).Hints(hints...).Grow()
}

func (m *Model) list() tui.View {
	if len(m.rows) == 0 {
		return tui.Center(m.emptyList())
	}
	playing := m.pl.State()
	return tui.Lazy(func(size tui.Size) tui.View {
		return tui.List(len(m.rows), m.cursor, &m.scroll, func(i int, sel bool) tui.View {
			r := m.rows[i]
			if r.track == nil {
				return m.groupRow(r, size.W)
			}
			return m.trackRow(*r.track, size.W, playing)
		}).Highlight(tui.Selection)
	})
}

func (m *Model) emptyList() tui.View {
	switch {
	case len(m.tracks) == 0:
		return tui.VStack(
			tui.Gradient("✦ your library is empty ✦").Bold(),
			tui.Text(""),
			tui.Text("press i to import a music folder").Fg(tui.Muted).Align(tui.Middle),
			tui.Text("files stay where they are; kyro only remembers them").Fg(tui.Muted).Align(tui.Middle),
		)
	case m.filter.Value() != "":
		return tui.Text("nothing matches “" + m.filter.Value() + "”").Fg(tui.Muted)
	case m.sec == secQueue:
		return tui.Text("the queue is empty — press e on a track to queue it").Fg(tui.Muted)
	case m.sec == secPlaylist:
		return tui.Text("empty playlist — press a on any track to add it here").Fg(tui.Muted)
	case m.sec == secLoved:
		return tui.Text("no loved songs yet — press l on a song to love it").Fg(tui.Muted)
	}
	return tui.Text("nothing here").Fg(tui.Muted)
}

// trackRow is "♪ Title   Artist   Album   3:42  FLAC 24/96", dropping
// columns as the width shrinks.
func (m *Model) trackRow(t library.Track, w int, s player.State) tui.View {
	now := s.Active && s.Track.ID == t.ID
	mark := tui.Text("  ")
	title := tui.Text(t.Title)
	if now {
		mark = tui.Text(m.eqGlyph() + " ").Fg(tui.Accent)
		title = title.Fg(tui.Accent).Bold()
	}
	if t.Missing {
		title = tui.Text(t.Title + " (missing)").Fg(tui.Muted)
	}
	cols := []tui.View{tui.Text(" "), mark, tui.Grow(3, title)}
	if w >= 60 {
		cols = append(cols, tui.Grow(2, tui.Text(" "+t.Artist).Fg(tui.Secondary)))
	}
	if w >= 90 {
		cols = append(cols, tui.Grow(2, tui.Text(" "+t.Album).Fg(tui.Muted)))
	}
	if w >= 70 {
		cols = append(cols, tui.Width(9, tui.HStack(tui.Text("  "), m.ratingView(t.Path, false))))
	}
	cols = append(cols, tui.Width(7, tui.Text(clock(t.Duration)).Fg(tui.Muted).Align(tui.End)))
	if w >= 76 {
		q := tui.Text(t.Quality()).Fg(tui.Muted)
		if t.HiRes() {
			q = q.Fg(tui.Accent3)
		}
		cols = append(cols, tui.Width(16, tui.HStack(tui.Text("  "), q)))
	}
	cols = append(cols, tui.Text(" "))
	return tui.HStack(cols...)
}

func (m *Model) groupRow(r row, w int) tui.View {
	icon := "◎"
	if m.sec == secArtists {
		icon = "☺"
	}
	cols := []tui.View{tui.Text(" "), tui.Text(icon + " ").Fg(tui.Accent), tui.Grow(3, tui.Text(r.group).Bold())}
	if r.sub != "" && w >= 50 {
		cols = append(cols, tui.Grow(2, tui.Text(" "+r.sub).Fg(tui.Secondary)))
	}
	return tui.HStack(append(cols, tui.Textf("%d tracks ", r.count).Fg(tui.Muted))...)
}

// ─── Now playing ────────────────────────────────────────────────────────────

// nowPlayingBar is the full layout's bottom panel.
func (m *Model) nowPlayingBar() tui.View {
	s := m.pl.State()
	body := tui.HStack(
		tui.Width(10, m.artView(s.Track, s.Active)),
		tui.Text("  "),
		tui.VStack(
			tui.HStack(tui.Grow(1, m.titleLine(s)), m.npRating(s), m.toggles(s)),
			m.subtitleLine(s),
			m.progress(s),
			tui.HStack(tui.Grow(1, m.qualityLine(s)), m.visualizer(s, 14)),
		).Grow(),
		tui.Text(" "),
	)
	return tui.Panel(tui.Pad(body, 0, 1, 0, 1)).Title("Now playing")
}

// npRating is the playing track's love and stars, beside the toggles.
func (m *Model) npRating(s player.State) tui.View {
	if !s.Active {
		return nil
	}
	return tui.HStack(m.ratingView(s.Track.Path, false), tui.Text("   "))
}

func (m *Model) titleLine(s player.State) tui.View {
	if !s.Active {
		return tui.Text("Nothing playing").Fg(tui.Muted).Bold()
	}
	return tui.Text(s.Track.Title).Bold()
}

func (m *Model) subtitleLine(s player.State) tui.View {
	if !s.Active {
		return tui.Text("pick a song and press ⏎").Fg(tui.Muted)
	}
	parts := []tui.Span{tui.S(orUnknown(s.Track.Artist, "Unknown artist"), tui.Fg(tui.Secondary))}
	if s.Track.Album != "" {
		parts = append(parts, tui.S(" — ", tui.Fg(tui.Muted)), tui.S(s.Track.Album, tui.Fg(tui.Muted)))
	}
	return tui.Line(parts...)
}

// qualityLine shows the stream and, when the device needs something
// different, what it actually receives: "FLAC 24/96 → 48 kHz".
func (m *Model) qualityLine(s player.State) tui.View {
	if !s.Active {
		return tui.Text("")
	}
	t := s.Track
	rate, ch := t.SampleRate, t.Channels
	if s.Source.SampleRate > 0 {
		rate, ch = s.Source.SampleRate, s.Source.Channels
	}
	spans := []tui.Span{tui.S(library.Quality(t.Codec, rate, t.BitDepth, ch), tui.Fg(qualityColor(t)))}
	if o := s.Output; o.SampleRate > 0 && (o.SampleRate != rate || o.Channels != ch) {
		out := fmt.Sprintf(" → %s kHz", trimKHz(o.SampleRate))
		if o.Channels != ch {
			out += " " + o.Layout
		}
		spans = append(spans, tui.S(out, tui.Fg(tui.Muted)))
	}
	if md := sound.Find(m.cfg.Audio.Sound); md.Chain != "" {
		spans = append(spans, tui.S(" · ", tui.Fg(tui.Muted)), tui.S("◆ "+md.Name, tui.Fg(tui.Accent2)))
	}
	if t.Year > 0 {
		spans = append(spans, tui.S(fmt.Sprintf(" · %d", t.Year), tui.Fg(tui.Muted)))
	}
	return tui.Line(spans...)
}

func qualityColor(t library.Track) tui.Color {
	if t.HiRes() {
		return tui.Accent3
	}
	return tui.Muted
}

func trimKHz(rate int) string {
	return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", float64(rate)/1000), "0"), ".")
}

func (m *Model) toggles(s player.State) tui.View {
	on := func(b bool) tui.Style {
		if b {
			return tui.Fg(tui.Accent2).Bold()
		}
		return tui.Fg(tui.Muted)
	}
	return tui.Line(
		tui.S("⇄", on(s.Shuffle)), tui.S("  ", tui.Style{}),
		tui.S(repeatGlyph(s.Repeat), on(s.Repeat != player.RepeatOff)), tui.S("  ", tui.Style{}),
		tui.S(fmt.Sprintf("♪ %d%%", s.Volume), tui.Fg(tui.Muted)),
	)
}

// progress is "▶ 1:23 ━━━━━━━●──────── 4:56", the filled part a gradient.
func (m *Model) progress(s player.State) tui.View {
	icon := "■"
	switch {
	case s.Active && s.Paused:
		icon = "❚❚"
	case s.Active:
		icon = "▶"
	}
	return tui.HStack(
		tui.Text(icon+" ").Fg(tui.Accent),
		tui.Text(clock(s.Pos)+" ").Fg(tui.Secondary),
		seekBar(s),
		tui.Text(" "+clock(s.Duration)).Fg(tui.Muted),
	)
}

func seekBar(s player.State) tui.View {
	return tui.Height(1, tui.Func(func(c tui.Ctx, size tui.Size) []string {
		w := size.W
		if w <= 0 {
			return nil
		}
		frac := 0.0
		if s.Duration > 0 {
			frac = math.Min(math.Max(float64(s.Pos)/float64(s.Duration), 0), 1)
		}
		filled := int(frac * float64(w))
		blend := lipgloss.Blend1D(max(w, 2), c.P.Accent, c.P.Accent2, c.P.Accent3)
		var b strings.Builder
		for i := 0; i < w; i++ {
			switch {
			case s.Active && i == filled:
				b.WriteString(lipgloss.NewStyle().Foreground(blend[i]).Bold(true).Render("●"))
			case i < filled:
				b.WriteString(lipgloss.NewStyle().Foreground(blend[i]).Render("━"))
			default:
				b.WriteString(lipgloss.NewStyle().Foreground(c.P.Border).Render("─"))
			}
		}
		return []string{b.String()}
	}))
}

// visualizer is a decorative level meter that dances while music plays and
// rests when paused.
func (m *Model) visualizer(s player.State, n int) tui.View {
	return tui.Frame(n, 1, tui.Func(func(c tui.Ctx, size tui.Size) []string {
		levels := []rune("▁▂▃▄▅▆▇")
		seed := float64(len(s.Track.Title) + len(s.Track.Artist))
		var b strings.Builder
		blend := lipgloss.Blend1D(max(size.W, 2), c.P.Accent3, c.P.Accent, c.P.Accent2)
		for i := 0; i < size.W; i++ {
			lv := 0
			if s.Active && !s.Paused {
				t := float64(m.tick) * 0.55
				v := (math.Sin(t+float64(i)*0.9+seed) + math.Sin(t*1.7+float64(i)*0.4) + 2) / 4
				lv = int(v * float64(len(levels)-1))
			}
			b.WriteString(lipgloss.NewStyle().Foreground(blend[i]).Render(string(levels[lv])))
		}
		return []string{b.String()}
	}))
}

// eqGlyph animates the playing track's marker.
func (m *Model) eqGlyph() string {
	if s := m.pl.State(); !s.Active || s.Paused {
		return "❚"
	}
	return []string{"▁", "▃", "▅", "▇", "▅", "▃"}[m.tick%6]
}

// artView draws the cover (or a generated placeholder) to fill its space.
func (m *Model) artView(t library.Track, active bool) tui.View {
	return tui.Func(func(c tui.Ctx, size tui.Size) []string {
		if !active {
			t = library.Track{Album: "kyro", Artist: "idle"}
		}
		lanes := c.P.Graph
		if len(lanes) < 3 {
			lanes = append(lanes, c.P.Accent, c.P.Accent2, c.P.Accent3)
		}
		mode := art.Mode(m.cfg.Art)
		var px *art.Pixels
		if mode == art.Auto || mode == art.PixelsMode {
			px = m.px // nil when the terminal can't show images
		}
		if mode == art.Auto {
			mode = art.Quadrant
		}
		return m.art.Cells(t, size.W, size.H, mode, lanes, px)
	})
}

func clock(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	d = d.Round(time.Second)
	h, mm, ss := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, mm, ss)
	}
	return fmt.Sprintf("%d:%02d", mm, ss)
}

// ─── Footer ─────────────────────────────────────────────────────────────────

func (m *Model) footer() tui.View {
	if m.filtering {
		return tui.HStack(tui.Text(" "), tui.Raw(m.filter.View()))
	}
	if m.flash != "" {
		c, icon := tui.Color(tui.Success), "✓ "
		if !m.flashOK {
			c, icon = tui.Danger, "✗ "
		}
		return tui.HStack(tui.Text(" "), tui.Text(icon+textx.FirstLine(m.flash)).Fg(c))
	}
	return tui.HStack(tui.Text(" "), tui.KeyHints(
		tui.H("space", "play"), tui.H("[ ]", "prev/next"), tui.H("←→", "seek"), tui.H("+-", "volume"),
		tui.H("l", "love"), tui.H(". ,", "stars"), tui.H("s", "shuffle"), tui.H("r", "repeat"),
		tui.H("z", "zen"), tui.H("i", "import"),
		tui.H("T", "theme"), tui.H("?", "help"), tui.H("q", "quit"),
	).Sep("  "))
}

// ─── Compact (a tiling pane) ────────────────────────────────────────────────

func (m *Model) compactView() tui.View {
	s := m.pl.State()
	cardH := 6
	if m.h < 14 {
		cardH = 4
	}
	artW := cardH * 2
	card := tui.HStack(
		tui.Width(artW, m.artView(s.Track, s.Active)),
		tui.Text("  "),
		tui.VStack(
			tui.HStack(tui.Grow(1, m.titleLine(s)), m.npRating(s), m.toggles(s)),
			m.subtitleLine(s),
			tui.If(cardH >= 6, tui.HStack(tui.Grow(1, m.qualityLine(s)), m.visualizer(s, 10))),
			tui.If(cardH >= 6, tui.Text("")),
			m.progress(s),
		).Grow(),
	)
	views := []tui.View{tui.Height(cardH, card)}
	if m.h-cardH >= 6 {
		views = append(views, tui.Panel(m.list()).
			Title(m.breadcrumb()).Label(tui.Text("tab ›").Fg(tui.Muted)).Focused(true).Grow())
	} else {
		views = append(views, tui.Spacer())
	}
	views = append(views, m.compactFooter())
	return tui.Pad(tui.VStack(views...), 0, 1)
}

func (m *Model) compactFooter() tui.View {
	if m.filtering || m.flash != "" {
		return m.footer()
	}
	return tui.KeyHints(tui.H("space", "play"), tui.H("[ ]", "skip"), tui.H("←→", "seek"),
		tui.H("tab", "section"), tui.H("z", "zen"), tui.H("?", "keys")).Sep("  ")
}

// ─── Mini (a sliver) ────────────────────────────────────────────────────────

func (m *Model) miniView() tui.View {
	s := m.pl.State()
	icon := "■"
	if s.Active {
		icon = map[bool]string{true: "❚❚", false: "▶"}[s.Paused]
	}
	title := "kyro — nothing playing"
	if s.Active {
		title = s.Track.Title
		if s.Track.Artist != "" {
			title += " — " + s.Track.Artist
		}
	}
	lines := []tui.View{tui.HStack(tui.Text(icon+" ").Fg(tui.Accent), tui.Text(title).Bold().Grow(),
		tui.Text(" "+clock(s.Pos)+"/"+clock(s.Duration)).Fg(tui.Muted))}
	if m.h >= 2 {
		lines = append(lines, seekBar(s))
	}
	if m.h >= 4 && m.flash != "" {
		lines = append(lines, m.footer())
	}
	return tui.VStack(lines...)
}

// ─── Zen ────────────────────────────────────────────────────────────────────

// zenView is the distraction-free player:
//
//	cover art        song
//	                 artist — album
//
//	            ◂◂   ▶   ▸▸
//	     1:23 ━━━━━━━●────────── 4:56
//	key bindings
func (m *Model) zenView() tui.View {
	s := m.pl.State()
	// Art takes most of the height; cells are ~2:1, so width = 2 × rows.
	artH := max(m.h-9, 4)
	artW := artH * 2
	stacked := m.w < 70
	if stacked {
		artH = max(min(m.h-12, (m.w-4)/2), 3)
		artW = artH * 2
	} else if artW > m.w*55/100 {
		artW = m.w * 55 / 100
		artH = artW / 2
	}
	cover := tui.Frame(artW, artH, m.artView(s.Track, s.Active))

	title := tui.View(tui.Gradient(orUnknown(s.Track.Title, "Nothing playing")).Bold())
	if !s.Active {
		title = tui.Text("Nothing playing").Fg(tui.Muted).Bold()
	}
	info := tui.VStack(
		title,
		m.subtitleLine(s),
		tui.Text(""),
		tui.If(s.Active, m.ratingView(s.Track.Path, true)),
		tui.If(s.Active, tui.Text("")),
		m.qualityLine(s),
		tui.Text(""),
		m.visualizer(s, 16),
	)

	var top tui.View
	if stacked {
		top = tui.VStack(tui.Center(cover), tui.Text(""), tui.Center(info))
	} else {
		top = tui.Center(tui.HStack(cover, tui.Text("    "), tui.Width(min(48, m.w-artW-8), tui.Center(info))))
	}

	play := "▶"
	if s.Active && !s.Paused {
		play = "❚❚"
	}
	controls := tui.Line(
		tui.S("◂◂", tui.Fg(tui.Secondary)), tui.S("     ", tui.Style{}),
		tui.S(play, tui.Fg(tui.Accent).Bold()), tui.S("     ", tui.Style{}),
		tui.S("▸▸", tui.Fg(tui.Secondary)),
	).Align(tui.Middle)
	barW := min(m.w-8, 72)
	bar := tui.Center(tui.Width(barW, tui.HStack(
		tui.Text(clock(s.Pos)+"  ").Fg(tui.Secondary), seekBar(s), tui.Text("  "+clock(s.Duration)).Fg(tui.Muted))))

	return tui.VStack(
		tui.Grow(1, top),
		tui.Height(1, controls),
		tui.Height(1, bar),
		tui.Height(1, tui.Center(m.toggles(s))),
		tui.Text(""),
		tui.Height(1, tui.Center(tui.KeyHints(
			tui.H("space", "play"), tui.H("[ ]", "prev/next"), tui.H("z", "exit zen"),
			tui.H("l", "love"), tui.H("0-5", "stars"), tui.H("a", "add to playlist"), tui.H("c", "cover colours"), tui.H("m", "sound"),
			tui.H("←→", "seek"), tui.H("+-", "volume"), tui.H("s", "shuffle"), tui.H("r", "repeat"),
		).Sep("  "))),
	)
}

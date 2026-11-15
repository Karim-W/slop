package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/kyro/internal/applemusic"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/apps/kyro/internal/player"
	"github.com/karim-w/slop/apps/kyro/internal/ratings"
	"github.com/karim-w/slop/pkg/tui"
)

// rateTarget is the track a love or rating key applies to: the selected
// row while browsing a list, otherwise (zen, sidebar, an album row) the one
// playing.
func (m *Model) rateTarget() (library.Track, bool) {
	if !m.zen && m.focus == focusList && m.cursor < len(m.rows) && m.rows[m.cursor].track != nil {
		return *m.rows[m.cursor].track, true
	}
	if s := m.pl.State(); s.Active {
		return s.Track, true
	}
	return library.Track{}, false
}

// rate changes a track's rating, saves it and tells hooks if it's playing.
func (m *Model) rate(t library.Track, fn func(*ratings.Rating)) (ratings.Rating, error) {
	if m.d.Ratings == nil {
		return ratings.Rating{}, errors.New("ratings are unavailable")
	}
	before := m.d.Ratings.Get(t.Path)
	r, err := m.d.Ratings.Update(t.Path, fn)
	if err == nil {
		m.rated(t, before, r)
		m.libRev++ // pages show loves and stars in their lists
	}
	if m.sec == secLoved {
		m.rebuild()
	}
	if s := m.pl.State(); s.Active && s.Track.Path == t.Path {
		m.announce(player.Change("rating"))
	}
	return r, err
}

// rateKey handles l (love), . and , (more / fewer stars) and, in zen, 0–5.
func (m *Model) rateKey(k string) (tea.Cmd, bool) {
	var fn func(*ratings.Rating)
	switch {
	case k == "l":
		fn = func(r *ratings.Rating) { r.Loved = !r.Loved }
	case k == ".":
		fn = func(r *ratings.Rating) { r.Stars++ }
	case k == ",":
		fn = func(r *ratings.Rating) { r.Stars-- }
	case m.zen && len(k) == 1 && k[0] >= '0' && k[0] <= '5':
		n := int(k[0] - '0')
		fn = func(r *ratings.Rating) { r.Stars = n }
	default:
		return nil, false
	}
	t, ok := m.rateTarget()
	if !ok {
		return m.showFlash("Nothing to rate: play a song or select one", false), true
	}
	r, err := m.rate(t, fn)
	if err != nil {
		return m.fail(err), true
	}
	what := strings.TrimSpace(stars(r.Stars, false))
	if r.Stars == 0 {
		what = "no stars"
	}
	if k == "l" {
		what = map[bool]string{true: "♥ Loved", false: "Unloved"}[r.Loved]
	}
	return tea.Batch(m.showFlash(fmt.Sprintf("%s · %s", what, t.Title), true), m.syncCmd()), true
}

func stars(n int, empty bool) string {
	if n <= 0 && !empty {
		return ""
	}
	return strings.Repeat("★", n) + strings.Repeat("☆", 5-n)
}

// ratingView is "♥ ★★★★☆" for a track (nothing for an unrated one unless
// always is set, as in zen mode).
func (m *Model) ratingView(path string, always bool) tui.View {
	r := m.d.Ratings.Get(path)
	var spans []tui.Span
	switch {
	case r.Loved:
		spans = append(spans, tui.S("♥ ", tui.Fg(tui.Danger)))
	case always:
		spans = append(spans, tui.S("♡ ", tui.Fg(tui.Muted)))
	}
	if r.Stars > 0 || always {
		spans = append(spans,
			tui.S(strings.Repeat("★", r.Stars), tui.Fg(tui.Warning)),
			tui.S(strings.Repeat("☆", 5-r.Stars), tui.Fg(tui.Border)))
	}
	if len(spans) == 0 {
		return tui.Text("")
	}
	return tui.Line(spans...)
}

// ─── Apple Music ────────────────────────────────────────────────────────────

type syncDoneMsg struct {
	synced int
	err    error
}

// syncCmd pushes unsynced ratings to Apple Music in the background.
func (m *Model) syncCmd() tea.Cmd {
	if m.d.Ratings == nil || m.cfg.AppleMusic == "off" || !applemusic.Available() {
		return nil
	}
	return m.syncPending(m.cfg.AppleMusic == "always")
}

func (m *Model) syncPending(launch bool) tea.Cmd {
	store, lib := m.d.Ratings, m.lib
	return func() tea.Msg {
		n := 0
		for _, path := range store.Unsynced() {
			r := store.Get(path)
			title := ""
			if t, ok := lib.ByPath(path); ok {
				title = t.Title
			}
			err := applemusic.Push(path, title, r.Stars, r.Loved, launch)
			switch {
			case errors.Is(err, applemusic.ErrNotRunning):
				return syncDoneMsg{synced: n} // catch up later
			case errors.Is(err, applemusic.ErrNotFound):
				store.MarkSynced(path) // not a Music track: nothing to sync
			case err != nil:
				store.Save()
				return syncDoneMsg{synced: n, err: err}
			default:
				store.MarkSynced(path)
				n++
			}
		}
		store.Save()
		return syncDoneMsg{synced: n}
	}
}

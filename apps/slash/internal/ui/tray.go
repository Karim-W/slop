package ui

import (
	"fmt"
	"slices"
	"time"

	"github.com/karim-w/slop/apps/slash/internal/transfer"
	"github.com/karim-w/slop/pkg/tui"
	"github.com/karim-w/slop/pkg/utils/humanize"
	"github.com/karim-w/slop/pkg/utils/mathx"
)

const (
	trayExpandedRows = 6
	rateSmoothing    = 0.3
)

// tray is the transfer progress panel at the bottom of the screen.
type tray struct {
	expanded   bool
	cursor     int
	selectedID int
	scroll     tui.Scroll
	items      []transfer.View
	summary    traySummary

	lastBytes    int64
	lastSampleAt time.Time
	rate         float64
}

type traySummary struct {
	active, queued, done, failed int
	totalBytes, doneBytes        int64
}

func (t *tray) update(stats transfer.Stats, now time.Time) {
	t.items = sortForDisplay(stats.Transfers)
	t.summary = summarize(t.items)
	if i := slices.IndexFunc(t.items, func(v transfer.View) bool { return v.ID == t.selectedID }); i >= 0 {
		t.cursor = i
	}
	t.cursor = min(t.cursor, max(len(t.items)-1, 0))
	t.sampleRate(stats.BytesTransferred, now)
}

func (t *tray) sampleRate(totalBytes int64, now time.Time) {
	if !t.lastSampleAt.IsZero() {
		elapsed := now.Sub(t.lastSampleAt).Seconds()
		if elapsed > 0 {
			instant := float64(totalBytes-t.lastBytes) / elapsed
			t.rate = rateSmoothing*instant + (1-rateSmoothing)*t.rate
		}
	}
	if t.summary.active == 0 {
		t.rate = 0
	}
	t.lastBytes = totalBytes
	t.lastSampleAt = now
}

func (t *tray) selected() (transfer.View, bool) {
	if t.cursor >= len(t.items) {
		return transfer.View{}, false
	}
	return t.items[t.cursor], true
}

func (t *tray) move(delta int) {
	t.cursor = min(max(t.cursor+delta, 0), max(len(t.items)-1, 0))
	if selected, ok := t.selected(); ok {
		t.selectedID = selected.ID
	}
}

func (t *tray) height() int {
	if !t.expanded {
		return 3
	}
	return trayExpandedRows + 3
}

// view is the tray panel: one overall progress line, plus a list of
// transfers when expanded.
func (t *tray) view(focused bool) tui.View {
	body := tui.VStack(t.overallLine())
	if t.expanded {
		list := tui.List(len(t.items), t.cursor, &t.scroll, func(i int, _ bool) tui.View {
			return transferRow(t.items[i])
		}).Empty(tui.Line(
			tui.S(" No transfers yet. Mark files and press ", tui.Fg(tui.Muted)),
			tui.S("c", tui.Fg(tui.Accent).Bold()),
			tui.S(" to copy them across.", tui.Fg(tui.Muted)),
		))
		if focused {
			list = list.Highlight(tui.Selection)
		}
		body = tui.VStack(t.overallLine(), list)
	}
	return tui.Height(t.height(), tui.Panel(body).Title("Transfers").Label(t.headline()).Focused(focused))
}

func (t *tray) headline() tui.View {
	s := t.summary
	var spans []tui.Span
	add := func(text string, c tui.Color) {
		if len(spans) > 0 {
			spans = append(spans, tui.S(" · ", tui.Fg(tui.Muted)))
		}
		spans = append(spans, tui.S(text, tui.Fg(c)))
	}
	if s.active > 0 {
		add(fmt.Sprintf("%d active", s.active), tui.Accent)
	}
	if s.queued > 0 {
		add(fmt.Sprintf("%d queued", s.queued), tui.Muted)
	}
	if s.done > 0 {
		add(fmt.Sprintf("%d done", s.done), tui.Success)
	}
	if s.failed > 0 {
		add(fmt.Sprintf("%d failed", s.failed), tui.Danger)
	}
	if len(spans) == 0 {
		return tui.Text("idle").Fg(tui.Muted)
	}
	return tui.Line(spans...)
}

func (t *tray) overallLine() tui.View {
	s := t.summary
	if s.totalBytes == 0 && s.active+s.queued == 0 {
		return tui.Text(" Nothing transferring").Fg(tui.Muted)
	}
	fraction := mathx.Fraction(s.doneBytes, s.totalBytes)
	eta := "--:--"
	if t.rate > 0 {
		eta = humanize.ETA(time.Duration(float64(s.totalBytes-s.doneBytes) / t.rate * float64(time.Second)))
	}
	stats := fmt.Sprintf(" %3.0f%%  %s / %s  %s  ETA %s ",
		fraction*100, humanize.Bytes(s.doneBytes), humanize.Bytes(s.totalBytes), humanize.Rate(t.rate), eta)
	return tui.HStack(tui.Text(" "), tui.ProgressBar(fraction), tui.Text(stats))
}

func transferRow(v transfer.View) tui.View {
	arrow, bar := tui.Text("↑").Fg(tui.Success), tui.Color(tui.Success)
	if v.Direction == transfer.Download {
		arrow, bar = tui.Text("↓").Fg(tui.Accent), tui.Accent
	}
	progress := v.Progress()
	var status tui.View
	switch v.State {
	case transfer.Queued:
		progress = 0
		status = tui.Text("queued · " + humanize.Bytes(v.Size)).Fg(tui.Muted)
	case transfer.Running:
		status = tui.Textf("%3.0f%%  %s  %s", progress*100, humanize.Rate(v.BytesPerSec), humanize.ETA(v.ETA))
	case transfer.Done:
		progress, bar = 1, tui.Success
		status = tui.Line(tui.S("✓ ", tui.Fg(tui.Success)), tui.S(humanize.Bytes(v.Size)+" · "+humanize.Rate(v.BytesPerSec), tui.Fg(tui.Muted)))
	case transfer.Canceled:
		bar = tui.Muted
		status = tui.Text("canceled").Fg(tui.Muted)
	case transfer.Failed:
		bar = tui.Danger
		status = tui.Text("✗ " + errorText(v.Err)).Fg(tui.Danger)
	}
	return tui.HStack(
		tui.Text(" "), arrow, tui.Text(" "),
		tui.Grow(1, tui.Text(v.Name)),
		tui.Text(" "),
		tui.Grow(2, tui.ProgressBar(progress).Color(bar)),
		tui.Text(" "),
		tui.Width(34, status),
	)
}

func errorText(err error) string {
	if err == nil {
		return "failed"
	}
	return err.Error()
}

func summarize(items []transfer.View) traySummary {
	var s traySummary
	for _, v := range items {
		switch v.State {
		case transfer.Running:
			s.active++
		case transfer.Queued:
			s.queued++
		case transfer.Done:
			s.done++
		case transfer.Failed:
			s.failed++
		}
		if v.State != transfer.Canceled && v.State != transfer.Failed {
			s.totalBytes += v.Size
			s.doneBytes += v.Transferred
		}
	}
	return s
}

// sortForDisplay shows what needs attention first: running, then queued,
// then failures, and finally the most recently finished transfers.
func sortForDisplay(items []transfer.View) []transfer.View {
	rank := map[transfer.State]int{
		transfer.Running: 0, transfer.Queued: 1, transfer.Failed: 2,
		transfer.Canceled: 3, transfer.Done: 4,
	}
	sorted := slices.Clone(items)
	slices.SortStableFunc(sorted, func(a, b transfer.View) int {
		if rank[a.State] != rank[b.State] {
			return rank[a.State] - rank[b.State]
		}
		if a.State.Finished() {
			return b.ID - a.ID
		}
		return a.ID - b.ID
	})
	return sorted
}

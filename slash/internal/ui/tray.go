package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/karim-w/slop/slash/internal/transfer"
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
	offset     int
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

func (t *tray) view(width int, focused bool) string {
	innerWidth := width - 2
	title := styleBold.Render("Transfers")
	if !t.expanded {
		return frame(title, t.headline(), []string{t.overallLine(innerWidth)}, width, t.height(), focused)
	}

	body := []string{t.overallLine(innerWidth)}
	rows := trayExpandedRows
	if len(t.items) == 0 {
		body = append(body, styleMuted.Render(" No transfers yet. Mark files and press ")+styleKey.Render("c")+styleMuted.Render(" to copy them across."))
	}
	t.scrollIntoView(rows)
	for i := t.offset; i < len(t.items) && i < t.offset+rows; i++ {
		line := transferRow(t.items[i], innerWidth)
		if focused && i == t.cursor {
			line = highlight(line, innerWidth, colorCursor)
		}
		body = append(body, line)
	}
	return frame(title, t.headline(), body, width, t.height(), focused)
}

func (t *tray) scrollIntoView(rows int) {
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+rows {
		t.offset = t.cursor - rows + 1
	}
	t.offset = min(t.offset, max(len(t.items)-rows, 0))
}

func (t *tray) headline() string {
	s := t.summary
	var parts []string
	if s.active > 0 {
		parts = append(parts, styleAccent.Render(fmt.Sprintf("%d active", s.active)))
	}
	if s.queued > 0 {
		parts = append(parts, styleMuted.Render(fmt.Sprintf("%d queued", s.queued)))
	}
	if s.done > 0 {
		parts = append(parts, styleSuccess.Render(fmt.Sprintf("%d done", s.done)))
	}
	if s.failed > 0 {
		parts = append(parts, styleError.Render(fmt.Sprintf("%d failed", s.failed)))
	}
	if len(parts) == 0 {
		return styleMuted.Render("idle")
	}
	return strings.Join(parts, styleMuted.Render(" · "))
}

func (t *tray) overallLine(width int) string {
	s := t.summary
	if s.totalBytes == 0 && s.active+s.queued == 0 {
		return styleMuted.Render(" Nothing transferring")
	}
	fraction := 0.0
	if s.totalBytes > 0 {
		fraction = float64(s.doneBytes) / float64(s.totalBytes)
	}
	eta := "--:--"
	if t.rate > 0 {
		eta = formatETA(time.Duration(float64(s.totalBytes-s.doneBytes) / t.rate * float64(time.Second)))
	}
	stats := fmt.Sprintf(" %3.0f%%  %s / %s  %s  ETA %s ",
		fraction*100, formatBytes(s.doneBytes), formatBytes(s.totalBytes), formatRate(t.rate), eta)
	barWidth := width - 2 - lipgloss.Width(stats)
	return " " + progressBar(fraction, barWidth, styleAccent) + styleText.Render(stats)
}

func transferRow(v transfer.View, width int) string {
	const statusWidth = 34
	arrow := styleSuccess.Render("↑")
	barStyle := styleSuccess
	if v.Direction == transfer.Download {
		arrow = styleAccent.Render("↓")
		barStyle = styleAccent
	}

	nameWidth := max(width/3, 16)
	barWidth := max(width-5-nameWidth-statusWidth, 6)
	name := fit(styleText.Render(v.Name), nameWidth)

	var bar, status string
	switch v.State {
	case transfer.Queued:
		bar = progressBar(0, barWidth, barStyle)
		status = styleMuted.Render("queued · " + formatBytes(v.Size))
	case transfer.Running:
		bar = progressBar(v.Progress(), barWidth, barStyle)
		status = fmt.Sprintf("%3.0f%%  %s  %s", v.Progress()*100, formatRate(v.BytesPerSec), formatETA(v.ETA))
	case transfer.Done:
		bar = progressBar(1, barWidth, styleSuccess)
		status = styleSuccess.Render("✓ ") + styleMuted.Render(formatBytes(v.Size)+" · "+formatRate(v.BytesPerSec))
	case transfer.Canceled:
		bar = progressBar(v.Progress(), barWidth, styleMuted)
		status = styleMuted.Render("canceled")
	case transfer.Failed:
		bar = progressBar(v.Progress(), barWidth, styleError)
		status = styleError.Render("✗ " + errorText(v.Err))
	}
	return " " + arrow + " " + name + " " + bar + " " + fit(status, statusWidth)
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

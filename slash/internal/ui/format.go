package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, exp := float64(n)/unit, 0
	for value >= unit && exp < 4 {
		value /= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", value, "KMGTP"[exp])
}

func formatRate(bytesPerSec float64) string {
	return formatBytes(int64(bytesPerSec)) + "/s"
}

func formatETA(d time.Duration) string {
	if d <= 0 {
		return "--:--"
	}
	d = d.Round(time.Second)
	hours, minutes, seconds := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

func formatModTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if t.Year() == time.Now().Year() {
		return t.Format("Jan 02 15:04")
	}
	return t.Format("Jan 02  2006")
}

// fit pads or truncates s so it occupies exactly width terminal cells.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) > width {
		return ansi.Truncate(s, width, "…")
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}

func fitLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) > width {
		return ansi.Truncate(s, width, "…")
	}
	return strings.Repeat(" ", width-lipgloss.Width(s)) + s
}

// truncateMiddle keeps both ends of a path visible: /home/…/project/src
func truncateMiddle(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width || width < 5 {
		return s
	}
	keep := width - 1
	head := keep / 3
	tail := keep - head
	return string(runes[:head]) + "…" + string(runes[len(runes)-tail:])
}

func progressBar(fraction float64, width int, fill lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	fraction = min(max(fraction, 0), 1)
	filled := int(fraction * float64(width))
	return fill.Render(strings.Repeat("━", filled)) +
		lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("━", width-filled))
}

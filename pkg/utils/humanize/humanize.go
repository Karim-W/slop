// Package humanize formats sizes, rates and times for people.
package humanize

import (
	"fmt"
	"time"
)

// Bytes renders n in binary units: 512 B, 1.5 KB, 3.2 GB.
func Bytes(n int64) string {
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

// Rate renders a transfer speed: 1.5 MB/s.
func Rate(bytesPerSec float64) string { return Bytes(int64(bytesPerSec)) + "/s" }

// ETA renders a remaining duration as m:ss or h:mm:ss, or --:-- when unknown.
func ETA(d time.Duration) string {
	if d <= 0 {
		return "--:--"
	}
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// ModTime renders a file timestamp like ls: the time this year, the year otherwise.
func ModTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if t.Year() == time.Now().Year() {
		return t.Format("Jan 02 15:04")
	}
	return t.Format("Jan 02  2006")
}

// Ago renders a compact age: now, 5m, 3h, 2d, 4w, Jan 2, Jan 2006.
func Ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/24/7))
	case d < 365*24*time.Hour:
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2006")
}

// AgoLong renders a spelled-out age: just now, 1 minute ago, 3 days ago.
func AgoLong(t time.Time) string {
	d := time.Since(t)
	unit := func(n int, s string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s ago", s)
		}
		return fmt.Sprintf("%d %ss ago", n, s)
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return unit(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return unit(int(d.Hours()), "hour")
	case d < 30*24*time.Hour:
		return unit(int(d.Hours()/24), "day")
	case d < 365*24*time.Hour:
		return unit(int(d.Hours()/24/30), "month")
	}
	return unit(int(d.Hours()/24/365), "year")
}

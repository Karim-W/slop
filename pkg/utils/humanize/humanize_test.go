package humanize

import (
	"testing"
	"time"
)

func TestFormats(t *testing.T) {
	cases := []struct{ got, want string }{
		{Bytes(512), "512 B"},
		{Bytes(1536), "1.5 KB"},
		{Bytes(3 << 30), "3.0 GB"},
		{Rate(2 << 20), "2.0 MB/s"},
		{ETA(0), "--:--"},
		{ETA(75 * time.Second), "1:15"},
		{ETA(time.Hour + 2*time.Minute + 3*time.Second), "1:02:03"},
		{Ago(time.Now().Add(-5 * time.Minute)), "5m"},
		{AgoLong(time.Now().Add(-3 * 24 * time.Hour)), "3 days ago"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
}

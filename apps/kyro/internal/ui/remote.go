package ui

import (
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/engine"
)

// remote acts on a media key, a headphone button or the system's Now
// Playing controls, exactly like the matching key in kyro.
func (m *Model) remote(cmd string) error {
	s := m.pl.State()
	switch cmd {
	case engine.RemoteToggle:
		return m.pl.Toggle()
	case engine.RemotePlay:
		if !s.Active || s.Paused {
			return m.pl.Toggle() // resumes, or starts the queue
		}
	case engine.RemotePause:
		return m.pl.SetPaused(true)
	case engine.RemoteStop:
		return m.pl.Stop()
	case engine.RemoteNext:
		return m.pl.Next()
	case engine.RemotePrev:
		return m.pl.Prev()
	case engine.RemoteForward:
		return m.pl.SeekBy(10 * time.Second)
	case engine.RemoteRewind:
		return m.pl.SeekBy(-10 * time.Second)
	}
	return nil
}

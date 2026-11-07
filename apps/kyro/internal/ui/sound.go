package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/karim-w/slop/apps/kyro/internal/sound"
)

// setSound switches the sound mode now (not saved; callers save).
func (m *Model) setSound(name string) tea.Cmd {
	md := sound.Find(name)
	m.cfg.Audio.Sound = md.Name
	if md.Name == "pure" {
		m.cfg.Audio.Sound = "" // the default stays out of config.json
	}
	return m.fail(m.pl.SetFilter(sound.Filter(md.Name)))
}

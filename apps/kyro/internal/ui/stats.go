package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/karim-w/slop/apps/kyro/internal/engine"
	"github.com/karim-w/slop/apps/kyro/internal/library"
	"github.com/karim-w/slop/apps/kyro/internal/player"
	"github.com/karim-w/slop/apps/kyro/internal/ratings"
	"github.com/karim-w/slop/apps/kyro/internal/sound"
	"github.com/karim-w/slop/apps/kyro/internal/stats"
)

// trace feeds an engine event to the listening-stats tracker.
func (m *Model) trace(e engine.Event, c player.Change) {
	if m.d.Stats == nil {
		return
	}
	s := m.pl.State()
	switch e.Kind {
	case engine.Position:
		if s.Active {
			m.tracker.Position(e.Pos)
		}
	case engine.Duration:
		if p := m.tracker.Current(); p != nil && p.Duration == 0 {
			p.Duration = e.Duration
		}
	case engine.Ended:
		switch {
		case e.Reason == engine.EOF:
			m.tracker.EOF()
		case e.Reason == engine.Error:
			m.record(m.tracker.Finish(stats.Failed))
		case !s.Active: // stopped, not replaced by another song
			m.record(m.tracker.Finish(stats.Stopped))
		}
	case engine.Exited:
		m.record(m.tracker.Finish(stats.Failed))
	}
	switch c {
	case player.TrackStarted:
		m.record(m.tracker.Begin(m.playOf(s)))
	case player.PausedChange:
		m.tracker.Paused()
	case player.Stopped:
		m.record(m.tracker.Finish(stats.Stopped))
	}
}

// playOf describes the song that just started, as the stats keep it.
func (m *Model) playOf(s player.State) stats.Play {
	t := s.Track
	r := m.d.Ratings.Get(t.Path)
	return stats.Play{
		TrackID: t.ID, Path: t.Path, Title: t.Title, Artist: t.Artist, Album: t.Album,
		AlbumArtist: t.AlbumArtist, Genre: t.Genre, Year: t.Year, Duration: t.Duration,
		Context: m.playCtx[0], ContextName: m.playCtx[1],
		Shuffle: s.Shuffle, Repeat: string(s.Repeat), Sound: sound.Find(m.cfg.Audio.Sound).Name,
		Codec: t.Codec, SampleRate: t.SampleRate, BitDepth: t.BitDepth, Channels: t.Channels,
		Loved: r.Loved, Stars: r.Stars,
	}
}

func (m *Model) record(p *stats.Play) {
	if p != nil {
		m.d.Stats.AddPlay(*p)
	}
}

// context names the list playback is starting from.
func (m *Model) context() [2]string {
	kind := strings.ToLower(sectionNames[m.sec])
	switch {
	case m.sec == secPlaylist:
		return [2]string{"playlist", m.playlist}
	case m.drill != nil && m.sec == secAlbums:
		return [2]string{"album", m.drill.group}
	case m.drill != nil && m.sec == secArtists:
		return [2]string{"artist", m.drill.group}
	}
	return [2]string{kind, ""}
}

// event logs something done to a song.
func (m *Model) event(kind, value string, t library.Track) {
	if m.d.Stats == nil {
		return
	}
	m.d.Stats.AddEvent(stats.Event{At: time.Now(), Kind: kind, Value: value, TrackID: t.ID,
		Path: t.Path, Title: t.Title, Artist: t.Artist, Album: t.Album})
}

// rated logs what changed between two ratings.
func (m *Model) rated(t library.Track, before, after ratings.Rating) {
	if before.Loved != after.Loved {
		m.event(map[bool]string{true: "love", false: "unlove"}[after.Loved], "", t)
	}
	if before.Stars != after.Stars {
		m.event("rate", strconv.Itoa(after.Stars), t)
	}
}

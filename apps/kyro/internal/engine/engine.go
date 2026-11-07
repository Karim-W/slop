// Package engine plays audio. The Engine interface hides the backend; the
// real one drives mpv, which decodes every format kyro indexes (MP3, AAC,
// ALAC, FLAC, WAV, AIFF, Opus, Vorbis, DSD, WavPack, AC-3, E-AC-3, TrueHD…)
// at full resolution and any channel count.
package engine

import "time"

// Kind is what an Event reports.
type Kind int

const (
	// Started: a file began playing (also after a gapless transition).
	Started Kind = iota
	// Position: playback position moved (at most a few times a second).
	Position
	// Duration: the file's length became known.
	Duration
	// Paused: pause state changed.
	Paused
	// Params: the decoded stream or the device format changed.
	Params
	// Ended: a file stopped: it finished, failed, or was replaced.
	Ended
	// Exited: the backend went away (crashed or was closed).
	Exited
	// Remote: a media key or the system's Now Playing controls asked for
	// Command. The engine doesn't act on it; the player decides.
	Remote
)

// Remote commands, from media keys, headphones, Control Center or the
// Touch Bar.
const (
	RemotePlay    = "play"
	RemotePause   = "pause"
	RemoteToggle  = "toggle"
	RemoteStop    = "stop"
	RemoteNext    = "next"
	RemotePrev    = "prev"
	RemoteForward = "forward"
	RemoteRewind  = "rewind"
)

// EndReason says why a file stopped.
type EndReason string

const (
	EOF      EndReason = "eof"
	Error    EndReason = "error"
	Stopped  EndReason = "stop"
	Replaced EndReason = "replaced"
)

// AudioParams describes a PCM stream.
type AudioParams struct {
	SampleRate int    `json:"sampleRate"`
	Channels   int    `json:"channels"`
	Layout     string `json:"layout"` // "stereo", "5.1(side)", …
	Format     string `json:"format"` // "s16", "s32", "float", …
}

// Event is something the backend reports.
type Event struct {
	Kind     Kind
	Path     string
	Pos      time.Duration
	Duration time.Duration
	Paused   bool
	// Source is the decoded stream; Output is what goes to the device
	// (different when the device can't take the source's rate or layout).
	Source, Output AudioParams
	Reason         EndReason
	// Continues is set on Ended when a gapless follow-up is queued, so a
	// Started for it comes next.
	Continues bool
	Err       error
	Command   string // for Remote: one of the Remote… commands
}

// Engine plays one file at a time with an optional gapless follow-up.
type Engine interface {
	// Play replaces whatever is playing with path.
	Play(path string) error
	// Next sets the file to continue with, gaplessly, once the current one
	// ends ("" clears it).
	Next(path string) error
	SetPaused(bool) error
	// Seek moves to an absolute position.
	Seek(time.Duration) error
	// SetVolume sets 0–100. Engines never amplify above 100.
	SetVolume(int) error
	// SetFilter sets the audio filter chain (a sound mode; "" for none).
	SetFilter(af string) error
	Stop() error
	Events() <-chan Event
	Close() error
}

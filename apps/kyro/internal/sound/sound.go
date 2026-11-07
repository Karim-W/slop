// Package sound holds kyro's sound modes: optional tone shaping applied by
// the engine, from "pure" (the file untouched) to modes that add punch,
// sub-bass or a wider stereo stage.
//
// Every shaped mode is built to be safe for speakers and ears:
//
//   - it lowers the level first (headroom), then boosts, so most music
//     never comes near full scale;
//   - it ends in a limiter that holds peaks at -1 dBFS, so the output is
//     never louder than a normal, loud master;
//   - nothing compresses the music as a whole, so drops keep their impact;
//   - the volume cap (100) still applies after it.
package sound

import "strings"

// Mode is one sound mode.
type Mode struct {
	Name string // stored in config.json as audio.sound
	Desc string // one line for the picker
	// Chain is the ffmpeg filter graph, empty for pure.
	Chain string
}

// limiter holds peaks at -1 dBFS (0.891) without auto make-up gain.
const limiter = "alimiter=limit=0.891:attack=5:release=60:level=false"

// Modes are the choices, in picker order.
var Modes = []Mode{
	{
		Name: "pure",
		Desc: "the file as mastered, untouched",
	},
	{
		Name: "punch",
		Desc: "tighter kick and bass, clearer attack and air; any output",
		Chain: "volume=-3dB," +
			"equalizer=f=60:t=o:w=1.2:g=3," + // sub weight
			"equalizer=f=120:t=o:w=1:g=4," + // kick body
			"equalizer=f=3200:t=o:w=1.5:g=1.5," + // attack, presence
			"treble=g=1.5:f=10000," + // air
			limiter,
	},
	{
		Name: "wide",
		Desc: "punch tuned for laptop speakers, with a wider stage",
		Chain: "volume=-4dB," +
			// Laptop woofers can't move much below ~80 Hz, so the weight
			// goes where they can play it and the ear hears it as bass.
			"equalizer=f=90:t=o:w=1:g=3," +
			"equalizer=f=150:t=o:w=1:g=3," +
			"equalizer=f=3200:t=o:w=1.5:g=1.5," +
			"treble=g=2:f=10000," +
			"stereotools=slev=1.4," + // more side, same centre: vocals stay put
			limiter,
	},
	{
		Name: "deep",
		Desc: "big sub-bass for headphones and external speakers",
		Chain: "volume=-5dB," +
			"bass=g=6:f=80:w=0.6," +
			"equalizer=f=45:t=o:w=1:g=2," +
			"equalizer=f=3200:t=o:w=1.5:g=1," +
			"treble=g=1.5:f=10000," +
			limiter,
	},
}

// Find returns the named mode, or pure.
func Find(name string) Mode {
	for _, m := range Modes {
		if strings.EqualFold(m.Name, name) {
			return m
		}
	}
	return Modes[0]
}

// Filter is the mode's value for mpv's af property ("" for pure).
func Filter(name string) string {
	m := Find(name)
	if m.Chain == "" {
		return ""
	}
	return "lavfi=[" + m.Chain + "]"
}

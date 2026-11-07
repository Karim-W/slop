// Package theme is the shared colour model of the slop design language: a
// Theme is a set of semantic colour roles (text, accent, success, …) as hex
// strings, plus the built-in palettes and the maths to blend and derive them.
// It has no terminal or UI dependencies; pkg/tui turns a Theme into styles.
package theme

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Theme is a complete colour scheme. Every field is a hex colour.
type Theme struct {
	Name      string   `json:"name"`
	Dark      bool     `json:"dark"`
	Bg        string   `json:"bg"`
	Surface   string   `json:"surface"` // panels, chips
	Overlay   string   `json:"overlay"` // modals
	Border    string   `json:"border"`  // inactive borders
	Text      string   `json:"text"`    // primary text
	Subtext   string   `json:"subtext"` // secondary text
	Muted     string   `json:"muted"`   // hints, timestamps
	Accent    string   `json:"accent"`  // primary brand colour
	Accent2   string   `json:"accent2"` // gradient partner
	Accent3   string   `json:"accent3"` // gradient partner
	Success   string   `json:"success"`
	Warning   string   `json:"warning"`
	Error     string   `json:"error"`
	Info      string   `json:"info"`
	Added     string   `json:"added"`
	Removed   string   `json:"removed"`
	Modified  string   `json:"modified"`
	Selection string   `json:"selection"` // selected row background
	Graph     []string `json:"graph"`     // lane colours
}

func (t Theme) C(hex string) color.Color { return Hex(hex) }

// Hex parses #rrggbb (or #rgb) into a colour.
func Hex(s string) color.Color {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 6 {
		return color.RGBA{R: 255, G: 0, B: 255, A: 255}
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}
}

// Mix blends a toward b by t (0 = a, 1 = b).
func Mix(a, b color.Color, t float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	l := func(x, y uint32) uint8 {
		return uint8((float64(x>>8)*(1-t) + float64(y>>8)*t) + 0.5)
	}
	return color.RGBA{R: l(ar, br), G: l(ag, bg), B: l(ab, bb), A: 255}
}

func ToHex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// Dimmed returns a copy with every foreground colour pulled toward the
// background. Used as a scrim behind modals.
func (t Theme) Dimmed(amount float64) Theme {
	bg := Hex(t.Bg)
	d := func(s string) string { return ToHex(Mix(Hex(s), bg, amount)) }
	o := t
	o.Surface, o.Border, o.Text, o.Subtext, o.Muted = d(t.Surface), d(t.Border), d(t.Text), d(t.Subtext), d(t.Muted)
	o.Accent, o.Accent2, o.Accent3 = d(t.Accent), d(t.Accent2), d(t.Accent3)
	o.Success, o.Warning, o.Error, o.Info = d(t.Success), d(t.Warning), d(t.Error), d(t.Info)
	o.Added, o.Removed, o.Modified, o.Selection = d(t.Added), d(t.Removed), d(t.Modified), d(t.Selection)
	o.Graph = make([]string, len(t.Graph))
	for i, g := range t.Graph {
		o.Graph[i] = d(g)
	}
	return o
}

// ─── Registry ───────────────────────────────────────────────────────────────

// All returns the built-in themes plus any user themes found as *.json in
// dir (pass "" to skip). User themes override built-ins with the same name,
// and fields a user theme leaves out inherit from the first built-in.
func All(dir string) []Theme {
	byName := map[string]Theme{}
	var order []string
	for _, t := range Builtins {
		byName[t.Name] = t
		order = append(order, t.Name)
	}
	var files []string
	if dir != "" {
		files, _ = filepath.Glob(filepath.Join(dir, "*.json"))
		sort.Strings(files)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		t := Builtins[0]
		t.Name, t.Graph = "", nil // a file without a name is named after itself
		if json.Unmarshal(b, &t) != nil {
			continue
		}
		if t.Name == "" {
			t.Name = strings.TrimSuffix(filepath.Base(f), ".json")
		}
		if len(t.Graph) == 0 {
			t.Graph = Builtins[0].Graph
		}
		if _, ok := byName[t.Name]; !ok {
			order = append(order, t.Name)
		}
		byName[t.Name] = t
	}
	out := make([]Theme, 0, len(order))
	for _, n := range order {
		out = append(out, byName[n])
	}
	return out
}

// Find looks a theme up by name, case-insensitively. It returns the first
// theme and false when nothing matches.
func Find(all []Theme, name string) (Theme, bool) {
	for _, t := range all {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return all[0], false
}

// Default is the theme apps start with when nothing else is configured.
func Default() Theme { return Builtins[0] }

// ─── Built-ins ──────────────────────────────────────────────────────────────

var Builtins = []Theme{
	{
		Name: "nebula", Dark: true,
		Bg: "#0d0e1a", Surface: "#171932", Overlay: "#1c1f3d", Border: "#2a2d52",
		Text: "#e6e6fa", Subtext: "#a9abd6", Muted: "#5d6093",
		Accent: "#a78bfa", Accent2: "#f472b6", Accent3: "#22d3ee",
		Success: "#4ade80", Warning: "#fbbf24", Error: "#fb7185", Info: "#60a5fa",
		Added: "#4ade80", Removed: "#fb7185", Modified: "#fbbf24", Selection: "#24264a",
		Graph: []string{"#a78bfa", "#22d3ee", "#f472b6", "#4ade80", "#fbbf24", "#60a5fa", "#fb923c", "#2dd4bf", "#e879f9"},
	},
	{
		Name: "catppuccin-mocha", Dark: true,
		Bg: "#1e1e2e", Surface: "#313244", Overlay: "#181825", Border: "#45475a",
		Text: "#cdd6f4", Subtext: "#bac2de", Muted: "#6c7086",
		Accent: "#cba6f7", Accent2: "#f5c2e7", Accent3: "#89dceb",
		Success: "#a6e3a1", Warning: "#f9e2af", Error: "#f38ba8", Info: "#89b4fa",
		Added: "#a6e3a1", Removed: "#f38ba8", Modified: "#f9e2af", Selection: "#313244",
		Graph: []string{"#cba6f7", "#89dceb", "#f5c2e7", "#a6e3a1", "#fab387", "#89b4fa", "#f9e2af", "#94e2d5", "#eba0ac"},
	},
	{
		Name: "tokyo-night", Dark: true,
		Bg: "#1a1b26", Surface: "#24283b", Overlay: "#16161e", Border: "#3b4261",
		Text: "#c0caf5", Subtext: "#a9b1d6", Muted: "#565f89",
		Accent: "#7aa2f7", Accent2: "#bb9af7", Accent3: "#7dcfff",
		Success: "#9ece6a", Warning: "#e0af68", Error: "#f7768e", Info: "#7dcfff",
		Added: "#9ece6a", Removed: "#f7768e", Modified: "#e0af68", Selection: "#283457",
		Graph: []string{"#7aa2f7", "#bb9af7", "#7dcfff", "#9ece6a", "#ff9e64", "#f7768e", "#e0af68", "#73daca", "#2ac3de"},
	},
	{
		Name: "rose-pine", Dark: true,
		Bg: "#191724", Surface: "#1f1d2e", Overlay: "#26233a", Border: "#403d52",
		Text: "#e0def4", Subtext: "#908caa", Muted: "#6e6a86",
		Accent: "#c4a7e7", Accent2: "#ebbcba", Accent3: "#9ccfd8",
		Success: "#9ccfd8", Warning: "#f6c177", Error: "#eb6f92", Info: "#31748f",
		Added: "#9ccfd8", Removed: "#eb6f92", Modified: "#f6c177", Selection: "#2a283e",
		Graph: []string{"#c4a7e7", "#ebbcba", "#9ccfd8", "#f6c177", "#eb6f92", "#31748f"},
	},
	{
		Name: "kanagawa", Dark: true,
		Bg: "#1f1f28", Surface: "#2a2a37", Overlay: "#16161d", Border: "#54546d",
		Text: "#dcd7ba", Subtext: "#c8c093", Muted: "#727169",
		Accent: "#7e9cd8", Accent2: "#957fb8", Accent3: "#7fb4ca",
		Success: "#98bb6c", Warning: "#e6c384", Error: "#e82424", Info: "#7fb4ca",
		Added: "#76946a", Removed: "#c34043", Modified: "#dca561", Selection: "#2d4f67",
		Graph: []string{"#7e9cd8", "#957fb8", "#7fb4ca", "#98bb6c", "#ffa066", "#d27e99", "#e6c384", "#6a9589"},
	},
	{
		Name: "gruvbox", Dark: true,
		Bg: "#1d2021", Surface: "#282828", Overlay: "#32302f", Border: "#504945",
		Text: "#ebdbb2", Subtext: "#d5c4a1", Muted: "#7c6f64",
		Accent: "#fe8019", Accent2: "#fabd2f", Accent3: "#8ec07c",
		Success: "#b8bb26", Warning: "#fabd2f", Error: "#fb4934", Info: "#83a598",
		Added: "#b8bb26", Removed: "#fb4934", Modified: "#fabd2f", Selection: "#3c3836",
		Graph: []string{"#fe8019", "#83a598", "#d3869b", "#b8bb26", "#fabd2f", "#8ec07c", "#fb4934"},
	},
	{
		Name: "nord", Dark: true,
		Bg: "#2e3440", Surface: "#3b4252", Overlay: "#434c5e", Border: "#4c566a",
		Text: "#eceff4", Subtext: "#d8dee9", Muted: "#7b88a1",
		Accent: "#88c0d0", Accent2: "#81a1c1", Accent3: "#8fbcbb",
		Success: "#a3be8c", Warning: "#ebcb8b", Error: "#bf616a", Info: "#5e81ac",
		Added: "#a3be8c", Removed: "#bf616a", Modified: "#ebcb8b", Selection: "#3b4252",
		Graph: []string{"#88c0d0", "#b48ead", "#a3be8c", "#ebcb8b", "#d08770", "#81a1c1", "#bf616a", "#8fbcbb"},
	},
	{
		Name: "dracula", Dark: true,
		Bg: "#282a36", Surface: "#343746", Overlay: "#21222c", Border: "#44475a",
		Text: "#f8f8f2", Subtext: "#e2e2dc", Muted: "#6272a4",
		Accent: "#bd93f9", Accent2: "#ff79c6", Accent3: "#8be9fd",
		Success: "#50fa7b", Warning: "#f1fa8c", Error: "#ff5555", Info: "#8be9fd",
		Added: "#50fa7b", Removed: "#ff5555", Modified: "#ffb86c", Selection: "#44475a",
		Graph: []string{"#bd93f9", "#8be9fd", "#ff79c6", "#50fa7b", "#ffb86c", "#f1fa8c", "#ff5555"},
	},
	{
		Name: "everforest", Dark: true,
		Bg: "#272e33", Surface: "#2e383c", Overlay: "#374145", Border: "#495156",
		Text: "#d3c6aa", Subtext: "#c5b99b", Muted: "#859289",
		Accent: "#a7c080", Accent2: "#83c092", Accent3: "#7fbbb3",
		Success: "#a7c080", Warning: "#dbbc7f", Error: "#e67e80", Info: "#7fbbb3",
		Added: "#a7c080", Removed: "#e67e80", Modified: "#dbbc7f", Selection: "#374145",
		Graph: []string{"#a7c080", "#7fbbb3", "#d699b6", "#dbbc7f", "#e69875", "#83c092", "#e67e80"},
	},
	{
		Name: "synthwave", Dark: true,
		Bg: "#1a1028", Surface: "#261447", Overlay: "#2b1650", Border: "#4a2a7a",
		Text: "#fdf0ff", Subtext: "#d9b8f0", Muted: "#7c5a9e",
		Accent: "#ff2a6d", Accent2: "#ff9e00", Accent3: "#05d9e8",
		Success: "#72f1b8", Warning: "#fede5d", Error: "#ff2a6d", Info: "#05d9e8",
		Added: "#72f1b8", Removed: "#fe4450", Modified: "#fede5d", Selection: "#34205a",
		Graph: []string{"#ff2a6d", "#05d9e8", "#ff9e00", "#72f1b8", "#d1f7ff", "#b967ff", "#fede5d"},
	},
	{
		Name: "catppuccin-latte", Dark: false,
		Bg: "#eff1f5", Surface: "#e6e9ef", Overlay: "#dce0e8", Border: "#bcc0cc",
		Text: "#4c4f69", Subtext: "#5c5f77", Muted: "#9ca0b0",
		Accent: "#8839ef", Accent2: "#ea76cb", Accent3: "#04a5e5",
		Success: "#40a02b", Warning: "#df8e1d", Error: "#d20f39", Info: "#1e66f5",
		Added: "#40a02b", Removed: "#d20f39", Modified: "#df8e1d", Selection: "#dce0e8",
		Graph: []string{"#8839ef", "#04a5e5", "#ea76cb", "#40a02b", "#fe640b", "#1e66f5", "#df8e1d", "#179299"},
	},
	{
		Name: "paper", Dark: false,
		Bg: "#fbfaf7", Surface: "#f1efe9", Overlay: "#ffffff", Border: "#d9d5ca",
		Text: "#24292f", Subtext: "#4b5258", Muted: "#8c959f",
		Accent: "#0969da", Accent2: "#8250df", Accent3: "#1a7f37",
		Success: "#1a7f37", Warning: "#9a6700", Error: "#cf222e", Info: "#0969da",
		Added: "#1a7f37", Removed: "#cf222e", Modified: "#9a6700", Selection: "#e8eef7",
		Graph: []string{"#0969da", "#8250df", "#bf3989", "#1a7f37", "#bc4c00", "#0a7c86", "#9a6700"},
	},
}

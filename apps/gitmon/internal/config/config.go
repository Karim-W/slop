// Package config is gitmon's persisted settings, kept in
// $XDG_CONFIG_HOME/gitmon (or ~/.config/gitmon).
package config

import (
	"path/filepath"

	"github.com/karim-w/slop/pkg/infra/jsonfile"
	"github.com/karim-w/slop/pkg/infra/paths"
)

// App is the name gitmon's files live under.
const App = "gitmon"

// Dir is gitmon's config folder.
func Dir() string { return paths.Config(App) }

// ThemesDir holds user themes as *.json.
func ThemesDir() string { return filepath.Join(Dir(), "themes") }

// Config is the contents of config.json.
type Config struct {
	Theme    string `json:"theme"`
	FlatTree bool   `json:"flat_tree,omitempty"`
	// Commit picks the commit flow: "inline" (default, a small message box)
	// or "editor" (git commit in $GIT_EDITOR / core.editor, like lazygit).
	Commit string `json:"commit,omitempty"`
}

func path() string { return filepath.Join(Dir(), "config.json") }

// Load reads config.json. A missing or broken file yields the defaults.
func Load() Config {
	var c Config
	_, _ = jsonfile.Load(path(), &c)
	return c
}

// Save writes config.json.
func Save(c Config) error { return jsonfile.Save(path(), c, 0o644) }

// Package paths resolves where an app keeps its configuration, state and
// cache on disk, so every app in the monorepo lays its files out the same way.
//
// Config follows the XDG convention on every OS ($XDG_CONFIG_HOME or
// ~/.config), which is what most terminal tools do. NativeConfig uses the
// OS-specific location (~/Library/Application Support on macOS) for apps
// that already store data there.
package paths

import (
	"os"
	"path/filepath"
)

// Config returns $XDG_CONFIG_HOME/<app> or ~/.config/<app>.
func Config(app string) string {
	return xdg("XDG_CONFIG_HOME", filepath.Join(".config"), app)
}

// State returns $XDG_STATE_HOME/<app> or ~/.local/state/<app>. Logs and
// other files the user does not edit by hand go here.
func State(app string) string {
	return xdg("XDG_STATE_HOME", filepath.Join(".local", "state"), app)
}

// Cache returns $XDG_CACHE_HOME/<app> or ~/.cache/<app>.
func Cache(app string) string {
	return xdg("XDG_CACHE_HOME", ".cache", app)
}

// NativeConfig returns the OS's own config folder for app, such as
// ~/Library/Application Support/<app> on macOS and ~/.config/<app> on Linux.
func NativeConfig(app string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, app), nil
}

// Expand replaces a leading ~ with the user's home folder.
func Expand(path string) string {
	if path == "~" || len(path) > 1 && path[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

func xdg(env, fallback, app string) string {
	if dir := os.Getenv(env); dir != "" {
		return filepath.Join(dir, app)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback, app)
}

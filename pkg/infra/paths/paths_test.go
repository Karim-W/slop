package paths

import (
	"path/filepath"
	"testing"
)

func TestXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/cfg")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/me")
	if got := Config("app"); got != "/x/cfg/app" {
		t.Fatal(got)
	}
	if got := State("app"); got != filepath.Join("/home/me", ".local/state/app") {
		t.Fatal(got)
	}
	if got := Expand("~/a"); got != "/home/me/a" {
		t.Fatal(got)
	}
}

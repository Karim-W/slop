package applemusic

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParsePull(t *testing.T) {
	got := parsePull("/m/a.m4a\t80\ttrue\n/m/b.mp3\t60\tfalse\nbad line\n/m/c.flac\t0\ttrue\n")
	if len(got) != 3 || got[0].Stars != 4 || !got[0].Loved || got[1].Stars != 3 || got[2].Stars != 0 || !got[2].Loved {
		t.Fatalf("%+v", got)
	}
}

// The scripts must compile against the installed Music app's dictionary.
// Compiling reads the dictionary only: Music is not launched or touched.
func TestScriptsCompileAgainstMusic(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	if _, err := os.Stat("/System/Applications/Music.app"); err != nil {
		t.Skip("no Music app")
	}
	dir := t.TempDir()
	for name, src := range map[string]string{
		"push": strings.Replace(pushScript, "%s", "favorited", 1),
		"pull": strings.ReplaceAll(pullScript, "%s", "favorited"),
	} {
		in := filepath.Join(dir, name+".applescript")
		os.WriteFile(in, []byte(src), 0o644)
		if out, err := exec.Command("osacompile", "-o", filepath.Join(dir, name+".scpt"), in).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		dec, _ := exec.Command("osadecompile", filepath.Join(dir, name+".scpt")).Output()
		if strings.Contains(string(dec), "«") {
			t.Fatalf("%s has unresolved terms:\n%s", name, dec)
		}
	}
}

package theme

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestHexRoundTrip(t *testing.T) {
	if got := ToHex(Hex("#a78bfa")); got != "#a78bfa" {
		t.Fatal(got)
	}
	if got := ToHex(Hex("#fff")); got != "#ffffff" {
		t.Fatal(got)
	}
	if got := ToHex(Mix(Hex("#000000"), Hex("#ffffff"), 0.5)); got != "#808080" {
		t.Fatal(got)
	}
}

func TestUserThemesOverrideAndInherit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mine.json"), []byte(`{"accent":"#ff0000"}`), 0o644)
	all := All(dir)
	mine, ok := Find(all, "MINE")
	if !ok || mine.Accent != "#ff0000" || mine.Bg != Default().Bg || len(mine.Graph) == 0 {
		t.Fatalf("%+v", mine)
	}
	if len(all) != len(Builtins)+1 {
		t.Fatalf("got %d themes", len(all))
	}
}

func TestFromTerminalPicksReadableColours(t *testing.T) {
	tc := TermColors{Bg: color.RGBA{0x01, 0x16, 0x27, 0xff}, Fg: color.RGBA{0xd6, 0xde, 0xeb, 0xff}}
	tc.ANSI[5] = color.RGBA{0xc7, 0x92, 0xea, 0xff}
	th := FromTerminal("rex", tc)
	if th.Name != "rex" || !th.Dark || th.Bg != "#011627" || th.Accent != "#c792ea" {
		t.Fatalf("%+v", th)
	}
}

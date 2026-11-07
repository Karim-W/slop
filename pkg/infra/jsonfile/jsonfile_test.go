package jsonfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cfg.json")
	type cfg struct{ Name string }
	var got cfg
	if found, err := Load(path, &got); found || err != nil {
		t.Fatalf("missing file: found=%v err=%v", found, err)
	}
	if err := Save(path, cfg{"slop"}, 0o600); err != nil {
		t.Fatal(err)
	}
	if found, err := Load(path, &got); !found || err != nil || got.Name != "slop" {
		t.Fatalf("found=%v err=%v got=%+v", found, err, got)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

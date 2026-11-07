package transfer

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/karim-w/slop/slash/internal/vfs"
)

func TestManagerCopiesNestedTreeAcrossChunks(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	big := randomBytes(t, 2*chunkSize+12345)
	writeFile(t, filepath.Join(src, "tree", "big.bin"), big)
	writeFile(t, filepath.Join(src, "tree", "nested", "small.txt"), []byte("hello"))
	writeFile(t, filepath.Join(src, "tree", "empty"), nil)
	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(src, "tree", "big.bin"), mtime, mtime); err != nil {
		t.Fatal(err)
	}

	local := vfs.NewLocal()
	m := NewManager(local, local, []vfs.FileSystem{local, local, local, local})
	defer m.Close()

	m.Enqueue(Request{Direction: Upload, Sources: []string{filepath.Join(src, "tree")}, DestDir: dst})
	stats := waitUntilSettled(t, m, 3)

	for _, v := range stats.Transfers {
		if v.State != Done {
			t.Fatalf("%s ended in state %d: %v", v.Name, v.State, v.Err)
		}
	}
	assertFile(t, filepath.Join(dst, "tree", "big.bin"), big)
	assertFile(t, filepath.Join(dst, "tree", "nested", "small.txt"), []byte("hello"))
	assertFile(t, filepath.Join(dst, "tree", "empty"), nil)

	info, err := os.Stat(filepath.Join(dst, "tree", "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(mtime) {
		t.Errorf("modification time not preserved: got %v want %v", info.ModTime(), mtime)
	}
}

func TestManagerReportsMissingSource(t *testing.T) {
	local := vfs.NewLocal()
	m := NewManager(local, local, []vfs.FileSystem{local})
	defer m.Close()

	m.Enqueue(Request{Direction: Download, Sources: []string{filepath.Join(t.TempDir(), "missing")}, DestDir: t.TempDir()})
	stats := waitUntilSettled(t, m, 1)
	if stats.Transfers[0].State != Failed || stats.Transfers[0].Err == nil {
		t.Fatalf("expected a failed transfer with an error, got %+v", stats.Transfers[0])
	}
}

func waitUntilSettled(t *testing.T, m *Manager, want int) Stats {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		stats := m.Stats()
		if len(stats.Transfers) == want && allFinished(stats.Transfers) {
			return stats
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("transfers did not settle: %+v", m.Stats().Transfers)
	return Stats{}
}

func allFinished(views []View) bool {
	for _, v := range views {
		if !v.State.Finished() {
			return false
		}
	}
	return true
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	data := make([]byte, n)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: content mismatch (got %d bytes, want %d)", path, len(got), len(want))
	}
}

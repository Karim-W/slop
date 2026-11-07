package vfs

import (
	"os"
	"path/filepath"
	"time"
)

type Local struct{}

func NewLocal() *Local { return &Local{} }

func (*Local) Label() string { return "Local" }

func (*Local) Home() (string, error) { return os.UserHomeDir() }

func (*Local) Join(elem ...string) string { return filepath.Join(elem...) }
func (*Local) Parent(path string) string  { return filepath.Dir(path) }
func (*Local) Base(path string) string    { return filepath.Base(path) }

func (l *Local) List(dir string) ([]Entry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		entry, err := l.Stat(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	SortEntries(entries)
	return entries, nil
}

func (*Local) Stat(path string) (Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, err
	}
	isLink := info.Mode()&os.ModeSymlink != 0
	if isLink {
		if target, err := os.Stat(path); err == nil {
			info = target
		}
	}
	return entryFromInfo(info, isLink), nil
}

func (*Local) Mkdir(path string) error                { return os.Mkdir(path, 0o755) }
func (*Local) MkdirAll(path string) error             { return os.MkdirAll(path, 0o755) }
func (*Local) Rename(from, to string) error           { return os.Rename(from, to) }
func (*Local) RemoveAll(path string) error            { return os.RemoveAll(path) }
func (*Local) OpenRead(path string) (ReadFile, error) { return os.Open(path) }

func (*Local) OpenWrite(path string) (WriteFile, error) {
	return os.OpenFile(path, os.O_WRONLY, 0)
}

func (*Local) CreateSized(path string, size int64) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := file.Truncate(size); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func (*Local) SetModTime(path string, mtime time.Time) error {
	return os.Chtimes(path, mtime, mtime)
}

func entryFromInfo(info os.FileInfo, isLink bool) Entry {
	return Entry{
		Name:    info.Name(),
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
		IsLink:  isLink,
	}
}

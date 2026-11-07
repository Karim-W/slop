// Package vfs gives the local disk and a remote SFTP server one shared
// interface so the browser panes and the transfer engine never need to
// care which side of the connection they are working on.
package vfs

import (
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"
)

type Entry struct {
	Name    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	IsDir   bool
	IsLink  bool
}

type ReadFile interface {
	io.ReaderAt
	io.Closer
}

type WriteFile interface {
	io.WriterAt
	io.Closer
}

type FileSystem interface {
	Label() string
	Home() (string, error)

	Join(elem ...string) string
	Parent(path string) string
	Base(path string) string

	List(dir string) ([]Entry, error)
	Stat(path string) (Entry, error)

	Mkdir(path string) error
	MkdirAll(path string) error
	Rename(from, to string) error
	RemoveAll(path string) error

	OpenRead(path string) (ReadFile, error)
	// CreateSized creates or truncates path and grows it to size, so that
	// several writers can later fill disjoint ranges of it in parallel.
	CreateSized(path string, size int64) error
	OpenWrite(path string) (WriteFile, error)
	SetModTime(path string, mtime time.Time) error
}

type WalkFunc func(path string, entry Entry) error

// Walk visits root and everything below it, parents before children.
func Walk(fsys FileSystem, root string, visit WalkFunc) error {
	entry, err := fsys.Stat(root)
	if err != nil {
		return err
	}
	return walk(fsys, root, entry, visit)
}

func walk(fsys FileSystem, path string, entry Entry, visit WalkFunc) error {
	if err := visit(path, entry); err != nil {
		return err
	}
	if !entry.IsDir {
		return nil
	}
	children, err := fsys.List(path)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := walk(fsys, fsys.Join(path, child.Name), child, visit); err != nil {
			return err
		}
	}
	return nil
}

// SortEntries puts directories first, then orders names case-insensitively.
func SortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

func IsHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

package vfs

import (
	"os"
	"path"
	"time"

	"github.com/pkg/sftp"
)

type Remote struct {
	client *sftp.Client
	label  string
}

func NewRemote(client *sftp.Client, label string) *Remote {
	return &Remote{client: client, label: label}
}

func (r *Remote) Label() string { return r.label }

func (r *Remote) Home() (string, error) { return r.client.Getwd() }

func (*Remote) Join(elem ...string) string { return path.Join(elem...) }
func (*Remote) Parent(p string) string     { return path.Dir(p) }
func (*Remote) Base(p string) string       { return path.Base(p) }

func (r *Remote) List(dir string) ([]Entry, error) {
	infos, err := r.client.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(infos))
	for _, info := range infos {
		isLink := info.Mode()&os.ModeSymlink != 0
		if isLink {
			if target, err := r.client.Stat(path.Join(dir, info.Name())); err == nil {
				info = renamedInfo{FileInfo: target, name: info.Name()}
			}
		}
		entries = append(entries, entryFromInfo(info, isLink))
	}
	SortEntries(entries)
	return entries, nil
}

func (r *Remote) Stat(p string) (Entry, error) {
	info, err := r.client.Lstat(p)
	if err != nil {
		return Entry{}, err
	}
	isLink := info.Mode()&os.ModeSymlink != 0
	if isLink {
		if target, err := r.client.Stat(p); err == nil {
			info = renamedInfo{FileInfo: target, name: info.Name()}
		}
	}
	return entryFromInfo(info, isLink), nil
}

func (r *Remote) Mkdir(p string) error                { return r.client.Mkdir(p) }
func (r *Remote) MkdirAll(p string) error             { return r.client.MkdirAll(p) }
func (r *Remote) Rename(from, to string) error        { return r.client.Rename(from, to) }
func (r *Remote) RemoveAll(p string) error            { return r.client.RemoveAll(p) }
func (r *Remote) OpenRead(p string) (ReadFile, error) { return r.client.Open(p) }

func (r *Remote) OpenWrite(p string) (WriteFile, error) {
	return r.client.OpenFile(p, os.O_WRONLY)
}

func (r *Remote) CreateSized(p string, size int64) error {
	file, err := r.client.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	if err := file.Truncate(size); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func (r *Remote) SetModTime(p string, mtime time.Time) error {
	return r.client.Chtimes(p, mtime, mtime)
}

// renamedInfo keeps a symlink's own name while reporting its target's metadata.
type renamedInfo struct {
	os.FileInfo
	name string
}

func (r renamedInfo) Name() string { return r.name }

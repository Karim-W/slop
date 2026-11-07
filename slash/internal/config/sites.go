// Package config persists saved sites on disk and their passwords in the
// operating system's keychain. Secrets never touch the JSON file.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

const (
	DefaultPort        = 22
	DefaultConnections = 4
	MaxConnections     = 16
)

type Site struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	User             string `json:"user"`
	KeyPath          string `json:"keyPath,omitempty"`
	Connections      int    `json:"connections"`
	RememberPassword bool   `json:"rememberPassword"`
	LastRemoteDir    string `json:"lastRemoteDir,omitempty"`
}

func (s Site) Address() string {
	return s.Host + ":" + strconv.Itoa(s.Port)
}

func (s Site) DisplayName() string {
	if s.Name != "" {
		return s.Name
	}
	return fmt.Sprintf("%s@%s", s.User, s.Host)
}

func (s Site) WithDefaults() Site {
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	s.Connections = min(max(s.Connections, 1), MaxConnections)
	if s.ID == "" {
		s.ID = newID()
	}
	return s
}

type Store struct {
	path  string
	Sites []Site `json:"sites"`
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "slash", "sites.json"), nil
}

func Load(path string) (*Store, error) {
	store := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, store); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return store, nil
}

func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Find(id string) (Site, bool) {
	i := s.indexOf(id)
	if i < 0 {
		return Site{}, false
	}
	return s.Sites[i], true
}

func (s *Store) Upsert(site Site) {
	if i := s.indexOf(site.ID); i >= 0 {
		s.Sites[i] = site
		return
	}
	s.Sites = append(s.Sites, site)
}

func (s *Store) Delete(id string) {
	s.Sites = slices.DeleteFunc(s.Sites, func(site Site) bool { return site.ID == id })
}

func (s *Store) indexOf(id string) int {
	return slices.IndexFunc(s.Sites, func(site Site) bool { return site.ID == id })
}

func newID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

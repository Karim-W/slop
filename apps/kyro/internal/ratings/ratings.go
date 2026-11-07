// Package ratings keeps loves and star ratings in ~/.kyro/ratings.json,
// keyed by file path. They live apart from the library index so rescans,
// re-imports and moved sources never lose them.
package ratings

import (
	"sync"
	"time"

	"github.com/karim-w/slop/pkg/infra/jsonfile"
)

// Rating is what the user thinks of a track.
type Rating struct {
	Stars   int       `json:"stars,omitempty"` // 0–5
	Loved   bool      `json:"loved,omitempty"`
	Synced  bool      `json:"synced"` // pushed to Apple Music
	Updated time.Time `json:"updated"`
}

// Store is ratings.json.
type Store struct {
	mu   sync.RWMutex
	path string
	M    map[string]Rating `json:"ratings"`
}

// Open loads ratings.json; a missing file is an empty store.
func Open(path string) (*Store, error) {
	s := &Store{path: path, M: map[string]Rating{}}
	_, err := jsonfile.Load(path, s)
	if s.M == nil {
		s.M = map[string]Rating{}
	}
	return s, err
}

// A nil *Store reads as empty, so callers need no checks.

// Get returns a track's rating (zero if none).
func (s *Store) Get(path string) Rating {
	if s == nil {
		return Rating{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.M[path]
}

// Update changes a track's rating with fn, marks it unsynced and saves.
func (s *Store) Update(path string, fn func(*Rating)) (Rating, error) {
	s.mu.Lock()
	r := s.M[path]
	fn(&r)
	r.Stars = min(max(r.Stars, 0), 5)
	r.Synced, r.Updated = false, time.Now()
	s.M[path] = r
	s.mu.Unlock()
	return r, s.Save()
}

// Import sets a rating that came from elsewhere (Apple Music), already in sync.
func (s *Store) Import(path string, stars int, loved bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.M[path] = Rating{Stars: min(max(stars, 0), 5), Loved: loved, Synced: true, Updated: time.Now()}
}

// MarkSynced records that path's current rating reached Apple Music.
func (s *Store) MarkSynced(path string) {
	s.mu.Lock()
	if r, ok := s.M[path]; ok {
		r.Synced = true
		s.M[path] = r
	}
	s.mu.Unlock()
}

// Unsynced lists paths whose rating hasn't reached Apple Music yet.
func (s *Store) Unsynced() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for p, r := range s.M {
		if !r.Synced {
			out = append(out, p)
		}
	}
	return out
}

// Loved lists every loved track's path.
func (s *Store) Loved() map[string]bool {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]bool{}
	for p, r := range s.M {
		if r.Loved {
			out[p] = true
		}
	}
	return out
}

// Save writes ratings.json.
func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return jsonfile.Save(s.path, s, 0o644)
}

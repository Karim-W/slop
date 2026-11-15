package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sync"
	"time"

	"github.com/karim-w/slop/pkg/infra/jsonfile"
)

// Pairing works like Bluetooth: while the web dialog is open in kyro, a
// six-digit code is shown there (and in its QR code). A browser that sends
// it gets a long random key, once and for all: kept in a cookie and in
// the page's storage, so a cleared cookie comes back by itself. Only the
// key's SHA-256 is kept, in devices.json, and any browser can be unpaired
// from kyro. Five wrong codes close pairing until the dialog is opened
// again, so the code can't be guessed.

// maxFails is how many wrong codes close pairing.
const maxFails = 5

var (
	ErrPairingClosed = errors.New("pairing is closed: open the web screen in kyro (press w) to pair")
	ErrWrongCode     = errors.New("that code is wrong")
)

type pairing struct {
	mu      sync.Mutex
	path    string
	Devices []Paired `json:"devices"`
	code    string
	open    bool
	fails   int
	loaded  bool
	saved   time.Time // last write, so "last seen" is saved now and then
}

// Paired is a browser allowed in.
type Paired struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Hash    string    `json:"hash"` // hex SHA-256 of the key
	Created time.Time `json:"created"`
	Seen    time.Time `json:"seen"`
	Live    bool      `json:"-"` // has a page open now
}

func (p *pairing) load() {
	if p.loaded {
		return
	}
	p.loaded = true
	jsonfile.Load(p.path, p)
	for i := range p.Devices { // files from before browsers had IDs
		if p.Devices[i].ID == "" {
			p.Devices[i].ID = p.Devices[i].Hash[:12]
		}
	}
}

func (p *pairing) save() error {
	p.saved = time.Now()
	return jsonfile.Save(p.path, p, 0o600)
}

// Open starts a pairing window with a fresh code.
func (p *pairing) Open() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	p.code, p.open, p.fails = fmt.Sprintf("%06d", n.Int64()), true, 0
	return p.code
}

// Close ends the pairing window.
func (p *pairing) Close() {
	p.mu.Lock()
	p.open, p.code = false, ""
	p.mu.Unlock()
}

// Code is the current code, "" when pairing is closed.
func (p *pairing) Code() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.open {
		return ""
	}
	return p.code
}

// Pair trades a code for a key.
func (p *pairing) Pair(code, name string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.open {
		return "", ErrPairingClosed
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(p.code)) != 1 {
		p.fails++
		if p.fails >= maxFails {
			p.open, p.code = false, ""
			return "", ErrPairingClosed
		}
		return "", ErrWrongCode
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	p.load()
	now := time.Now()
	h := hash(token)
	p.Devices = append(p.Devices, Paired{ID: h[:12], Name: name, Hash: h, Created: now, Seen: now})
	if err := p.save(); err != nil {
		return "", err
	}
	return token, nil
}

// Check returns the browser a key belongs to.
func (p *pairing) Check(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	h := []byte(hash(token))
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	found := -1
	for i, d := range p.Devices {
		if subtle.ConstantTimeCompare(h, []byte(d.Hash)) == 1 {
			found = i
		}
	}
	if found < 0 {
		return "", false
	}
	p.Devices[found].Seen = time.Now()
	if time.Since(p.saved) > 10*time.Minute {
		p.save()
	}
	return p.Devices[found].ID, true
}

// List returns the paired browsers, most recently seen first.
func (p *pairing) List() []Paired {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	out := slices.Clone(p.Devices)
	slices.SortStableFunc(out, func(a, b Paired) int { return b.Seen.Compare(a.Seen) })
	return out
}

// Revoke unpairs one browser ("" unpairs all).
func (p *pairing) Revoke(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if id == "" {
		p.Devices = nil
	} else {
		p.Devices = slices.DeleteFunc(p.Devices, func(d Paired) bool { return d.ID == id })
	}
	return p.save()
}

// Flush saves "last seen" times.
func (p *pairing) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded && len(p.Devices) > 0 {
		p.save()
	}
}

func hash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

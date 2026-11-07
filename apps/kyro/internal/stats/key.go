package stats

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
)

// KeychainService and KeychainAccount name the database key in the system
// keychain (macOS Keychain, Secret Service on Linux).
const (
	KeychainService = "kyro-stats"
	KeychainAccount = "database-key"
)

// ErrKeyLost means the database exists but its key is gone from the
// keychain, so it can't be read. kyro leaves the file alone.
var ErrKeyLost = errors.New("the key for your listening stats is missing from the keychain")

// Keyring is where the key is kept; pkg/infra/keychain satisfies it.
type Keyring interface {
	Get(key string) (string, bool)
	Set(key, secret string) error
}

// LoadKey returns the database key, creating a random one the first time.
// It never replaces a key while a database made with it exists.
func LoadKey(kr Keyring, dbPath string) ([]byte, error) {
	if v, ok := kr.Get(KeychainAccount); ok {
		k, err := hex.DecodeString(v)
		if err != nil || len(k) != 32 {
			return nil, errors.New("the key for your listening stats in the keychain is damaged")
		}
		return k, nil
	}
	if fi, err := os.Stat(dbPath); err == nil && fi.Size() > 0 {
		return nil, ErrKeyLost
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := kr.Set(KeychainAccount, hex.EncodeToString(k)); err != nil {
		return nil, err
	}
	return k, nil
}

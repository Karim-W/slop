// Package keychain stores secrets in the operating system's credential store:
// the macOS Keychain, the Secret Service on Linux and the Windows Credential
// Manager. Secrets never touch config files.
package keychain

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// Store is one app's namespace in the keychain.
type Store struct{ service string }

// New returns the store for service, such as "slash-sftp".
func New(service string) Store { return Store{service: service} }

// Get returns the secret saved under key, and false if there is none.
func (s Store) Get(key string) (string, bool) {
	v, err := keyring.Get(s.service, key)
	if err != nil {
		return "", false
	}
	return v, true
}

// Set saves secret under key, replacing any earlier value.
func (s Store) Set(key, secret string) error { return keyring.Set(s.service, key, secret) }

// Delete removes key. Deleting a key that does not exist is not an error.
func (s Store) Delete(key string) error {
	err := keyring.Delete(s.service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

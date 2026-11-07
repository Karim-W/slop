package session

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// UnknownHostError means the server is not in known_hosts yet. The caller
// should show the fingerprint to the user and call TrustHost if accepted.
type UnknownHostError struct {
	Host string
	Key  ssh.PublicKey
}

func (e *UnknownHostError) Error() string {
	return fmt.Sprintf("unknown host %s (%s)", e.Host, e.Fingerprint())
}

func (e *UnknownHostError) Fingerprint() string {
	return ssh.FingerprintSHA256(e.Key)
}

var ErrHostKeyChanged = errors.New("host key has CHANGED since last connection: possible man-in-the-middle attack, refusing to connect")

func knownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

func hostKeyCallback() (ssh.HostKeyCallback, error) {
	path, err := knownHostsPath()
	if err != nil {
		return nil, err
	}
	if err := ensureFile(path); err != nil {
		return nil, err
	}
	verify, err := knownhosts.New(path)
	if err != nil {
		return nil, err
	}
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		err := verify(host, remote, key)
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return err
		}
		if len(keyErr.Want) == 0 {
			return &UnknownHostError{Host: host, Key: key}
		}
		return ErrHostKeyChanged
	}, nil
}

func TrustHost(unknown *UnknownHostError) error {
	path, err := knownHostsPath()
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	line := knownhosts.Line([]string{knownhosts.Normalize(unknown.Host)}, unknown.Key)
	_, err = fmt.Fprintln(file, line)
	return err
}

func ensureFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return err
	}
	return file.Close()
}

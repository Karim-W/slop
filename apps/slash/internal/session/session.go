// Package session opens SSH/SFTP connections to a saved site: one for
// browsing plus a pool of extra connections used for parallel transfers.
package session

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/karim-w/slop/apps/slash/internal/config"
)

var ErrAuthFailed = errors.New("authentication failed")

const dialTimeout = 15 * time.Second

type Session struct {
	Site    config.Site
	Browse  *sftp.Client
	Workers []*sftp.Client

	conns     []*ssh.Client
	agentConn net.Conn
}

// Open connects to site. The browse connection is dialled first so host key
// and authentication problems surface once, before the worker pool is built.
// If the server refuses extra connections, transfers share the browse one.
func Open(site config.Site, password string) (*Session, error) {
	s := &Session{Site: site}
	cfg, err := s.clientConfig(password)
	if err != nil {
		return nil, err
	}

	browseConn, browse, err := dial(site.Address(), cfg)
	if err != nil {
		s.Close()
		return nil, err
	}
	s.conns = append(s.conns, browseConn)
	s.Browse = browse

	s.Workers = s.dialWorkers(site.Address(), cfg, site.Connections)
	if len(s.Workers) == 0 {
		s.Workers = []*sftp.Client{browse}
	}
	return s, nil
}

func (s *Session) Label() string {
	return fmt.Sprintf("%s@%s", s.Site.User, s.Site.Host)
}

func (s *Session) Close() {
	for _, worker := range s.Workers {
		if worker != s.Browse {
			worker.Close()
		}
	}
	if s.Browse != nil {
		s.Browse.Close()
	}
	for _, conn := range s.conns {
		conn.Close()
	}
	if s.agentConn != nil {
		s.agentConn.Close()
	}
}

func (s *Session) dialWorkers(addr string, cfg *ssh.ClientConfig, count int) []*sftp.Client {
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		workers []*sftp.Client
	)
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, client, err := dial(addr, cfg)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			s.conns = append(s.conns, conn)
			workers = append(workers, client)
		}()
	}
	wg.Wait()
	return workers
}

func dial(addr string, cfg *ssh.ClientConfig) (*ssh.Client, *sftp.Client, error) {
	conn, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, nil, classifyDialError(err)
	}
	client, err := sftp.NewClient(conn,
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
		sftp.MaxConcurrentRequestsPerFile(64),
	)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("starting sftp subsystem: %w", err)
	}
	return conn, client, nil
}

func classifyDialError(err error) error {
	var unknown *UnknownHostError
	if errors.As(err, &unknown) {
		return unknown
	}
	if errors.Is(err, ErrHostKeyChanged) {
		return ErrHostKeyChanged
	}
	if strings.Contains(err.Error(), "unable to authenticate") {
		return ErrAuthFailed
	}
	return err
}

func (s *Session) clientConfig(password string) (*ssh.ClientConfig, error) {
	verifyHostKey, err := hostKeyCallback()
	if err != nil {
		return nil, fmt.Errorf("loading known_hosts: %w", err)
	}
	return &ssh.ClientConfig{
		User:            s.Site.User,
		Auth:            s.authMethods(password),
		HostKeyCallback: verifyHostKey,
		Timeout:         dialTimeout,
	}, nil
}

// authMethods tries, in order: ssh-agent, private keys, then the password.
// The password doubles as the passphrase for encrypted private keys.
func (s *Session) authMethods(password string) []ssh.AuthMethod {
	var methods []ssh.AuthMethod

	if signers := s.agentSigners(); signers != nil {
		methods = append(methods, ssh.PublicKeysCallback(signers))
	}
	if signers := loadKeySigners(s.keyPaths(), password); len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}
	if password != "" {
		methods = append(methods,
			ssh.Password(password),
			ssh.KeyboardInteractive(answerWithPassword(password)),
		)
	}
	return methods
}

func (s *Session) agentSigners() func() ([]ssh.Signer, error) {
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil
	}
	s.agentConn = conn
	return agent.NewClient(conn).Signers
}

func (s *Session) keyPaths() []string {
	if s.Site.KeyPath != "" {
		return []string{expandHome(s.Site.KeyPath)}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var paths []string
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		paths = append(paths, filepath.Join(home, ".ssh", name))
	}
	return paths
}

func loadKeySigners(paths []string, passphrase string) []ssh.Signer {
	var signers []ssh.Signer
	for _, path := range paths {
		pem, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(pem)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) && passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(passphrase))
		}
		if err == nil {
			signers = append(signers, signer)
		}
	}
	return signers
}

func answerWithPassword(password string) ssh.KeyboardInteractiveChallenge {
	return func(_, _ string, questions []string, _ []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i := range answers {
			answers[i] = password
		}
		return answers, nil
	}
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

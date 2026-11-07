package session_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/karim-w/slop/slash/internal/config"
	"github.com/karim-w/slop/slash/internal/session"
	"github.com/karim-w/slop/slash/internal/sftptest"
	"github.com/karim-w/slop/slash/internal/transfer"
	"github.com/karim-w/slop/slash/internal/vfs"
)

const (
	testUser     = "tester"
	testPassword = "s3cret"
)

func TestConnectTrustAndTransfer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")

	server, err := sftptest.Start("127.0.0.1:0", testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	site := config.Site{Host: server.Host, Port: server.Port, User: testUser, Connections: 4}.WithDefaults()

	_, err = session.Open(site, testPassword)
	var unknown *session.UnknownHostError
	if !errors.As(err, &unknown) {
		t.Fatalf("first connection should ask to trust the host, got %v", err)
	}
	if err := session.TrustHost(unknown); err != nil {
		t.Fatal(err)
	}

	if _, err := session.Open(site, "wrong"); !errors.Is(err, session.ErrAuthFailed) {
		t.Fatalf("wrong password should fail authentication, got %v", err)
	}

	sess, err := session.Open(site, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if len(sess.Workers) != 4 {
		t.Fatalf("expected 4 worker connections, got %d", len(sess.Workers))
	}

	local := vfs.NewLocal()
	workers := make([]vfs.FileSystem, len(sess.Workers))
	for i, client := range sess.Workers {
		workers[i] = vfs.NewRemote(client, sess.Label())
	}
	manager := transfer.NewManager(local, vfs.NewRemote(sess.Browse, sess.Label()), workers)
	defer manager.Close()

	localDir, remoteDir, downloadDir := t.TempDir(), t.TempDir(), t.TempDir()
	payload := make([]byte, 80<<20+777)
	_, _ = rand.Read(payload)
	source := filepath.Join(localDir, "payload.bin")
	if err := os.WriteFile(source, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	manager.Enqueue(transfer.Request{Direction: transfer.Upload, Sources: []string{source}, DestDir: remoteDir})
	waitForDone(t, manager, 1)
	manager.ClearFinished()

	manager.Enqueue(transfer.Request{Direction: transfer.Download, Sources: []string{filepath.Join(remoteDir, "payload.bin")}, DestDir: downloadDir})
	waitForDone(t, manager, 1)

	manager.ClearFinished()

	tree := filepath.Join(localDir, "project")
	for _, rel := range []string{"a.txt", "src/main.go", "src/deep/nested/file.bin"} {
		path := filepath.Join(tree, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(tree, "empty-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager.Enqueue(transfer.Request{Direction: transfer.Upload, Sources: []string{tree}, DestDir: remoteDir})
	waitForDone(t, manager, 3)
	manager.ClearFinished()
	manager.Enqueue(transfer.Request{Direction: transfer.Download, Sources: []string{filepath.Join(remoteDir, "project")}, DestDir: downloadDir})
	waitForDone(t, manager, 3)
	for _, rel := range []string{"a.txt", "src/main.go", "src/deep/nested/file.bin"} {
		data, err := os.ReadFile(filepath.Join(downloadDir, "project", rel))
		if err != nil || string(data) != rel {
			t.Fatalf("%s did not survive the directory round trip: %q, %v", rel, data, err)
		}
	}
	if info, err := os.Stat(filepath.Join(downloadDir, "project", "empty-dir")); err != nil || !info.IsDir() {
		t.Fatalf("empty directory was not copied: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(downloadDir, "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("round-tripped file differs from the original")
	}
}

func waitForDone(t *testing.T, m *transfer.Manager, want int) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		stats := m.Stats()
		done := 0
		for _, v := range stats.Transfers {
			if v.State == transfer.Failed {
				t.Fatalf("%s failed: %v", v.Name, v.Err)
			}
			if v.State == transfer.Done {
				done++
			}
		}
		if done == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for transfers: %+v", m.Stats().Transfers)
}

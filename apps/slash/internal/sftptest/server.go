// Package sftptest runs a throwaway in-process SSH/SFTP server on localhost
// for integration tests and local manual testing. It serves the real local
// filesystem, so point clients at temporary directories.
package sftptest

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Server struct {
	Host    string
	Port    int
	HostKey ssh.PublicKey

	listener net.Listener
	config   *ssh.ServerConfig
	wg       sync.WaitGroup
}

func Start(addr, user, password string) (*Server, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		return nil, err
	}

	config := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, given []byte) (*ssh.Permissions, error) {
			if meta.User() == user && string(given) == password {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		},
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	portNumber, _ := strconv.Atoi(port)

	s := &Server{Host: host, Port: portNumber, HostKey: signer.PublicKey(), listener: listener, config: config}
	s.wg.Add(1)
	go s.acceptLoop()
	return s, nil
}

func (s *Server) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

func (s *Server) Close() {
	s.listener.Close()
	s.wg.Wait()
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.serveConn(conn)
	}
}

func (s *Server) serveConn(conn net.Conn) {
	serverConn, channels, requests, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		conn.Close()
		return
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only sessions are supported")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go serveSFTP(channel, channelRequests)
	}
}

func serveSFTP(channel ssh.Channel, requests <-chan *ssh.Request) {
	for req := range requests {
		isSFTP := req.Type == "subsystem" && len(req.Payload) > 4 && string(req.Payload[4:]) == "sftp"
		_ = req.Reply(isSFTP, nil)
		if isSFTP {
			go runSFTP(channel)
		}
	}
}

func runSFTP(channel ssh.Channel) {
	defer channel.Close()
	server, err := sftp.NewServer(channel)
	if err != nil {
		return
	}
	if err := server.Serve(); err != nil && !errors.Is(err, io.EOF) {
		fmt.Println("sftptest:", err)
	}
}

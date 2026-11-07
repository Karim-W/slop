// Command devserver starts a local SFTP server for trying slash without a
// real remote machine:
//
//	go run ./cmd/devserver -addr 127.0.0.1:2222 -user dev -password dev
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"golang.org/x/crypto/ssh"

	"github.com/karim-w/slop/slash/internal/sftptest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:2222", "listen address")
	user := flag.String("user", "dev", "accepted user name")
	password := flag.String("password", "dev", "accepted password")
	flag.Parse()

	server, err := sftptest.Start(*addr, *user, *password)
	if err != nil {
		log.Fatal(err)
	}
	defer server.Close()

	fmt.Printf("SFTP server on %s (user %q, password %q)\n", server.Addr(), *user, *password)
	fmt.Printf("host key %s\n", ssh.FingerprintSHA256(server.HostKey))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
}

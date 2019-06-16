// Copyright 2018 The goftp Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// This is a very simple ftpd server using this library as an example
// and as something to run tests against.
package main

import (
    "testing"
	"log"

	"github.com/attenberger/ftps_qftp-server"
	"github.com/attenberger/ftps_qftp-server/ftps"
	filedriver "github.com/attenberger/goftp-file-driver"
)

func TestServer(t *testing.T) {
	root := "/home/admin/ftpsroot"
	user := "admin"
	pass := "admin"
	port := 2121
	host := "10.0.0.12"
	key := "/home/admin/key.pem"
	cert := "/home/admin/cert.pem"

	factory := &filedriver.FileDriverFactory{
		RootPath: root,
		Perm:     filedriver.NewSimplePerm("user", "group"),
	}

	opts := &ftps.ServerOpts{
		Factory:      factory,
		Port:         port,
		Hostname:     host,
		Auth:         &ftp_server.SimpleAuth{Name: user, Password: pass},
		TLS:          true,
		KeyFile:      key,
		CertFile:     cert,
		ExplicitFTPS: true,
		PassivePorts: "32500-33000",
	}

	log.Printf("Starting ftp server on %v:%v", opts.Hostname, opts.Port)
	log.Printf("Username %v, Password %v", user, pass)
	server := ftps.NewServer(opts)
	err := server.ListenAndServe()
	if err != nil {
		log.Fatal("Error starting server:", err)
	}
}

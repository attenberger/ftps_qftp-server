// Copyright 2018 The goftp Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

// This is a very simple ftpd server using this library as an example
// and as something to run tests against.
package main

import (
	"flag"
	"log"

	"github.com/attenberger/ftps_qftp-server"
	"github.com/attenberger/ftps_qftp-server/ftps"
	filedriver "github.com/attenberger/goftp-file-driver"
)

func main() {
	var (
		root = flag.String("root", "", "Root directory to serve")
		user = flag.String("user", "admin", "Username for login")
		pass = flag.String("pass", "123456", "Password for login")
		port = flag.Int("port", 2121, "Port")
		host = flag.String("host", "localhost", "Port")
		key  = flag.String("key", "", "Path to private key for TLS")
		cert = flag.String("cert", "", "Path to certificate for TLS")
	)
	flag.Parse()
	messageAboutMissingParameters := ""
	if *root == "" {
		messageAboutMissingParameters = messageAboutMissingParameters + "Please set a root to serve with -root\n"
	}
	if *key == "" {
		messageAboutMissingParameters = messageAboutMissingParameters + "Please set a keyfile for tls with -key\n"
	}
	if *cert == "" {
		messageAboutMissingParameters = messageAboutMissingParameters + "Please set a certificatefile for tls with -cert\n"
	}
	if messageAboutMissingParameters != "" {
		log.Fatalf(messageAboutMissingParameters)
	}

	factory := &filedriver.FileDriverFactory{
		RootPath: *root,
		Perm:     filedriver.NewSimplePerm("user", "group"),
	}

	opts := &ftps.ServerOpts{
		Factory:      factory,
		Port:         *port,
		Hostname:     *host,
		Auth:         &ftp_server.SimpleAuth{Name: *user, Password: *pass},
		TLS:          true,
		KeyFile:      *key,
		CertFile:     *cert,
		ExplicitFTPS: true,
	}

	log.Printf("Starting ftp server on %v:%v", opts.Hostname, opts.Port)
	log.Printf("Username %v, Password %v", *user, *pass)
	server := ftps.NewServer(opts)
	err := server.ListenAndServe()
	if err != nil {
		log.Fatal("Error starting server:", err)
	}
}

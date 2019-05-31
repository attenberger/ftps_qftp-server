// Copyright 2018 The goftp Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package ftpq

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	server "github.com/attenberger/ftps_qftp-server"
	"github.com/lucas-clemente/quic-go"
	"net"
	"strconv"
	"sync"
)

const (
	MaxStreamsPerSession = 3      // like default in vsftpd // but separate limit for uni- and bidirectional streams
	MaxStreamFlowControl = 212992 // like OpenSuse TCP /proc/sys/net/core/rmem_max
	KeepAlive            = false
)

// Version returns the library version
func Version() string {
	return "0.3.0"
}

// ServerOpts contains parameters for server.NewServer()
type ServerOpts struct {
	// The factory that will be used to create a new FTPDriver instance for
	// each client connection. This is a mandatory option.
	Factory server.DriverFactory

	Auth server.Auth

	// Server Name, Default is Go Ftp Server
	Name string

	// The hostname that the FTP server should listen on. Optional, defaults to
	// "::", which means all hostnames on ipv4 and ipv6.
	Hostname string

	// Public IP of the server
	PublicIp string

	// The port that the FTP should listen on. Optional, defaults to 3000. In
	// a production environment you will probably want to change this to 21.
	Port int

	// use tls, default is false
	TLS bool

	// if tls used, cert file is required
	CertFile string

	// if tls used, key file is required
	KeyFile string

	WelcomeMessage string

	// A logger implementation, if nil the StdLogger is used
	Logger server.Logger
}

// Server is the root of your FTP application. You should instantiate one
// of these and call ListenAndServe() to start accepting client connections.
//
// Always use the NewServer() method to create a new Server.
type Server struct {
	*ServerOpts
	listenTo   string
	logger     server.Logger
	listener   quic.Listener
	tlsConfig  *tls.Config
	quicConfig *quic.Config
	ctx        context.Context
	cancel     context.CancelFunc
	feats      string
}

// ErrServerClosed is returned by ListenAndServe() or Serve() when a shutdown
// was requested.
var ErrServerClosed = errors.New("quic-ftp: Server closed")

// serverOptsWithDefaults copies an ServerOpts struct into a new struct,
// then adds any default values that are missing and returns the new data.
func serverOptsWithDefaults(opts *ServerOpts) *ServerOpts {
	var newOpts ServerOpts
	if opts == nil {
		opts = &ServerOpts{}
	}
	if opts.Hostname == "" {
		newOpts.Hostname = "::"
	} else {
		newOpts.Hostname = opts.Hostname
	}
	if opts.Port == 0 {
		newOpts.Port = 3000
	} else {
		newOpts.Port = opts.Port
	}
	newOpts.Factory = opts.Factory
	if opts.Name == "" {
		newOpts.Name = "Go QUIC-FTP Server"
	} else {
		newOpts.Name = opts.Name
	}

	if opts.WelcomeMessage == "" {
		newOpts.WelcomeMessage = defaultWelcomeMessage
	} else {
		newOpts.WelcomeMessage = opts.WelcomeMessage
	}

	if opts.Auth != nil {
		newOpts.Auth = opts.Auth
	}

	newOpts.Logger = &server.StdLogger{}
	if opts.Logger != nil {
		newOpts.Logger = opts.Logger
	}

	newOpts.TLS = opts.TLS
	newOpts.KeyFile = opts.KeyFile
	newOpts.CertFile = opts.CertFile

	newOpts.PublicIp = opts.PublicIp

	return &newOpts
}

// NewServer initialises a new FTP server. Configuration options are provided
// via an instance of ServerOpts. Calling this function in your code will
// probably look something like this:
//
//     factory := &MyDriverFactory{}
//     server  := server.NewServer(&server.ServerOpts{ factory: factory })
//
// or:
//
//     factory := &MyDriverFactory{}
//     opts    := &server.ServerOpts{
//       factory: factory,
//       Port: 2000,
//       Hostname: "127.0.0.1",
//     }
//     server  := server.NewServer(opts)
//
func NewServer(opts *ServerOpts) *Server {
	opts = serverOptsWithDefaults(opts)
	s := new(Server)
	s.ServerOpts = opts
	s.listenTo = net.JoinHostPort(opts.Hostname, strconv.Itoa(opts.Port))
	s.logger = opts.Logger
	return s
}

// NewConn constructs a new object that will handle the FTP protocol over
// an active net.TCPConn. The TCP connection should already be open before
// it is handed to this functions. driver is an instance of FTPDriver that
// will handle all auth and persistence details.
func (server *Server) newConn(quicSession quic.Session, driver server.Driver) (*Conn, error) {
	c := new(Conn)
	c.factory = server.Factory
	c.session = quicSession
	c.dataReceiveStreams = map[quic.StreamID]quic.ReceiveStream{}
	c.structAccessMutex = sync.Mutex{}
	c.server = server
	c.sessionID = newSessionID()
	c.logger = server.logger
	c.runningSubConn = 0
	return c, nil
}

func simpleTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	config := &tls.Config{}
	if config.NextProtos == nil {
		config.NextProtos = []string{"ftp"}
	}

	var err error
	config.Certificates = make([]tls.Certificate, 1)
	config.Certificates[0], err = tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func simpleQUICConfig() *quic.Config {
	config := &quic.Config{}
	config.ConnectionIDLength = 4
	config.MaxIncomingUniStreams = MaxStreamsPerSession
	config.MaxIncomingStreams = MaxStreamsPerSession
	config.MaxReceiveStreamFlowControlWindow = MaxStreamFlowControl
	config.MaxReceiveConnectionFlowControlWindow = MaxStreamFlowControl * (MaxStreamsPerSession + 1) // + 1 buffer for controllstreams
	config.KeepAlive = KeepAlive
	return config
}

// ListenAndServe asks a new Server to begin accepting client connections. It
// accepts no arguments - all configuration is provided via the NewServer
// function.
//
// If the server fails to start for any reason, an error will be returned. Common
// errors are trying to bind to a privileged port or something else is already
// listening on the same port.
//
func (server *Server) ListenAndServe() error {
	var listener quic.Listener
	var err error
	var curFeats = featCmds

	server.tlsConfig, err = simpleTLSConfig(server.CertFile, server.KeyFile)
	if err != nil {
		return err
	}

	server.quicConfig = simpleQUICConfig()

	listener, err = quic.ListenAddr(server.listenTo, server.tlsConfig, server.quicConfig)
	if err != nil {
		return err
	}
	server.feats = fmt.Sprintf(feats, curFeats)

	sessionID := ""
	server.logger.Printf(sessionID, "%s listening on %d", server.Name, server.Port)

	return server.Serve(listener)
}

// Serve accepts connections on a given net.Listener and handles each
// request in a new goroutine.
//
func (server *Server) Serve(l quic.Listener) error {
	server.listener = l
	server.ctx, server.cancel = context.WithCancel(context.Background())
	sessionID := ""
	for {
		quicSession, err := server.listener.Accept()
		if err != nil {
			select {
			case <-server.ctx.Done():
				return ErrServerClosed
			default:
			}
			server.logger.Printf(sessionID, "listening error: %v", err)
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				continue
			}
			return err
		}
		driver, err := server.Factory.NewDriver()
		if err != nil {
			server.logger.Printf(sessionID, "Error creating driver, aborting client connection: %v", err)
			quicSession.Close()
		} else {
			ftpConn, err := server.newConn(quicSession, driver)
			if err != nil {
				server.logger.Printf(sessionID, "Error establishing new connection: %v", err)
				quicSession.Close()
				continue
			}
			go ftpConn.Serve()
		}
	}
}

// Shutdown will gracefully stop a server. Already connected clients will retain their connections
func (server *Server) Shutdown() error {
	if server.cancel != nil {
		server.cancel()
	}
	if server.listener != nil {
		return server.listener.Close()
	}
	// server wasnt even started
	return nil
}

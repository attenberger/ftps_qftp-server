// Copyright 2018 The goftp Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package server

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/lucas-clemente/quic-go"
	"io"
	"sync"
)

const (
	defaultWelcomeMessage = "Welcome to the Go QUIC-FTP Server"
)

type Conn struct {
	// The factory that will be used to create a new FTPDriver instance for
	// each client connection. This is a mandatory option.
	factory DriverFactory

	session            quic.Session
	dataReceiveStreams map[quic.StreamID]quic.ReceiveStream
	structAccessMutex  sync.Mutex
	logger             Logger
	server             *Server
	sessionID          string
	connRunningMutex   sync.Mutex
	runningSubConn     int
}

func (conn *Conn) PublicIp() string {
	return conn.server.PublicIp
}

func (conn *Conn) passiveListenIP() string {
	if len(conn.PublicIp()) > 0 {
		return conn.PublicIp()
	}
	return conn.session.LocalAddr().String()
}

// returns a random 20 char string that can be used as a unique session ID
func newSessionID() string {
	hash := sha256.New()
	_, err := io.CopyN(hash, rand.Reader, 50)
	if err != nil {
		return "????????????????????"
	}
	md := hash.Sum(nil)
	mdStr := hex.EncodeToString(md)
	return mdStr[0:20]
}

// NewSubConn constructs a new object that will handle the FTP protocol over
// an QUIC-Stream. The QUIC connection should already be open before
// it is handed to this functions. driver is an instance of FTPDriver that
// will handle all auth and persistence details.
func (conn *Conn) newSubConn(quicStream quic.Stream, driver Driver) *SubConn {
	subC := new(SubConn)
	subC.connection = conn
	subC.controlStream = quicStream
	subC.controlReader = bufio.NewReader(quicStream)
	subC.controlWriter = bufio.NewWriter(quicStream)
	subC.namePrefix = "/"
	subC.logger = &StdLogger{}
	subC.sessionID = conn.sessionID
	subC.driver = driver

	//driver.Init(c)
	return subC
}

// Serve starts an endless loop that reads FTP commands from the client and
// responds appropriately. terminated is a channel that will receive a true
// message when the connection closes. This loop will be running inside a
// goroutine, so use this channel to be notified when the connection can be
// cleaned up.
func (conn *Conn) Serve() {
	conn.logger.Print(conn.sessionID, "Connection Established")

	for {
		driver, err := conn.factory.NewDriver()
		if err != nil {
			conn.logger.Printf(conn.sessionID, "Error creating driver, aborting client connection: %v", err)
			conn.Close()
			return
		}

		controlStream, err := conn.session.AcceptStream()
		if err != nil && err.Error() != "NO_ERROR" {
			conn.logger.Print(conn.sessionID, fmt.Sprint("Error while accepting control stream, aborting client connection:", err))
			conn.Close()
			return
		}

		subConn := conn.newSubConn(controlStream, driver)
		conn.structAccessMutex.Lock()
		conn.runningSubConn++
		conn.structAccessMutex.Unlock()
		go subConn.Serve()
	}
}

// Close will manually close this connection, even if the client isn't ready.
func (conn *Conn) Close() {
	conn.session.Close()
	//conn.closed = true
}

// A subconnection should call this function while terminating.
// It is used to close the connection after all subconnections are closed.
func (conn *Conn) ReportSubConnFinsihed() {
	conn.structAccessMutex.Lock()
	conn.runningSubConn--
	if conn.runningSubConn == 0 {
		conn.Close()
		conn.logger.Print(conn.sessionID, "Connection Terminated")
		return
	}
	conn.structAccessMutex.Unlock()
}

// Accepts datastreams and returns the stream with the wanted ID.
func (conn *Conn) getReceiveDataStream(streamID quic.StreamID) (quic.ReceiveStream, error) {
	conn.structAccessMutex.Lock()
	defer conn.structAccessMutex.Unlock()
	stream, available := conn.dataReceiveStreams[streamID]
	if available {
		return stream, nil
	} else {
		for {
			stream, err := conn.session.AcceptUniStream()
			if err != nil {
				return nil, err
			}
			conn.dataReceiveStreams[stream.StreamID()] = stream
			if stream.StreamID() > streamID {
				return nil, errors.New("Could not get wanted stream.")
			} else if stream.StreamID() == streamID {
				return stream, nil
			}
		}
	}
}

// Opens a new datastream.
func (conn *Conn) getNewSendDataStream() (quic.SendStream, error) {
	conn.structAccessMutex.Lock()
	defer conn.structAccessMutex.Unlock()
	stream, err := conn.session.OpenUniStreamSync()
	if err != nil {
		return nil, err
	}
	return stream, nil
}

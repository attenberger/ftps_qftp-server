// Copyright 2018 The goftp Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package server

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/attenberger/quic-go"
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

	session quic.Session
	/*controlStream      quic.Stream
	controlReader      *bufio.Reader
	controlWriter      *bufio.Writer*/
	dataReceiveStreams map[quic.StreamID]quic.ReceiveStream
	structAccessMutex  sync.Mutex
	//dataConn      	DataSocket
	//driver      Driver
	auth      Auth
	logger    Logger
	server    *Server
	tlsConfig *tls.Config
	sessionID string
	//renameFrom  string
	//lastFilePos int64
	//appendData  bool
	closed           bool
	connRunningMutex sync.Mutex
	runningSubConn   int
}

/*
 */
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

	/*for i := 0; i < MaxStreamsPerSession; i++ {
		driver, err := conn.factory.NewDriver()
		if err != nil {
			conn.logger.Printf(conn.sessionID, "Error creating driver, aborting client connection: %v", err)
			conn.Close()
			return
		}

		controlStream, err := conn.session.OpenStreamSync()
		if err != nil {
			conn.logger.Print(conn.sessionID, fmt.Sprint("Error while opening main control stream, aborting client connection:", err))
			conn.Close()
			return
		}

		subConn := conn.newSubConn(controlStream, driver)
		conn.structAccessMutex.Lock()
		conn.runningSubConn++
		conn.structAccessMutex.Unlock()
		go subConn.Serve()
	}*/
}

// Close will manually close this connection, even if the client isn't ready.
func (conn *Conn) Close() {
	conn.session.Close()
	conn.closed = true
}

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

func (conn *Conn) GetNumberSubConn() int {
	conn.structAccessMutex.Lock()
	defer conn.structAccessMutex.Unlock()
	return conn.runningSubConn
}

/*// receiveLine accepts a single line FTP command and co-ordinates an
// appropriate response.
func (conn *Conn) receiveLine(line string) {
	command, param := conn.parseLine(line)
	conn.logger.PrintCommand(conn.sessionID, command, param)
	cmdObj := commands[strings.ToUpper(command)]
	if cmdObj == nil {
		conn.writeMessage(502, "Command not found")
		return
	}
	if cmdObj.RequireParam() && param == "" {
		conn.writeMessage(553, "action aborted, required param missing")
	} else if cmdObj.RequireAuth() && conn.user == "" {
		conn.writeMessage(530, "not logged in")
	} else {
		cmdObj.Execute(conn, param)
	}
}

func (conn *Conn) parseLine(line string) (string, string) {
	params := strings.SplitN(strings.Trim(line, "\r\n"), " ", 2)
	if len(params) == 1 {
		return params[0], ""
	}
	return params[0], strings.TrimSpace(params[1])
}

// writeMessage will send a standard FTP response back to the client.
func (conn *Conn) writeMessage(code int, message string) (wrote int, err error) {
	conn.logger.PrintResponse(conn.sessionID, code, message)
	line := fmt.Sprintf("%d %s\r\n", code, message)
	wrote, err = conn.controlWriter.WriteString(line)
	conn.controlWriter.Flush()
	return
}

// writeMessage will send a standard FTP response back to the client.
func (conn *Conn) writeMessageMultiline(code int, message string) (wrote int, err error) {
	conn.logger.PrintResponse(conn.sessionID, code, message)
	line := fmt.Sprintf("%d-%s\r\n%d END\r\n", code, message, code)
	wrote, err = conn.controlWriter.WriteString(line)
	conn.controlWriter.Flush()
	return
}

// buildPath takes a client supplied path or filename and generates a safe
// absolute path within their account sandbox.
//
//    buildpath("/")
//    => "/"
//    buildpath("one.txt")
//    => "/one.txt"
//    buildpath("/files/two.txt")
//    => "/files/two.txt"
//    buildpath("files/two.txt")
//    => "/files/two.txt"
//    buildpath("/../../../../etc/passwd")
//    => "/etc/passwd"
//
// The driver implementation is responsible for deciding how to treat this path.
// Obviously they MUST NOT just read the path off disk. The probably want to
// prefix the path with something to scope the users access to a sandbox.
func (conn *Conn) buildPath(filename string) (fullPath string) {
	if len(filename) > 0 && filename[0:1] == "/" {
		fullPath = filepath.Clean(filename)
	} else if len(filename) > 0 && filename != "-a" {
		fullPath = filepath.Clean(conn.namePrefix + "/" + filename)
	} else {
		fullPath = filepath.Clean(conn.namePrefix)
	}
	fullPath = strings.Replace(fullPath, "//", "/", -1)
	fullPath = strings.Replace(fullPath, string(filepath.Separator), "/", -1)
	return
}

// sendOutofbandData will send a string to the client via the currently open
// data socket. Assumes the socket is open and ready to be used.
func (conn *Conn) sendOutofbandData(data []byte, stream quic.SendStream) quic.StreamID {
	bytes := len(data)
	stream.Write(data)
	streamID := stream.StreamID()
	stream.Close()
	message := "Closing data strea,, sent " + strconv.Itoa(bytes) + " bytes"
	conn.writeMessage(226, message)

	return streamID
}

func (conn *Conn) sendOutofBandDataWriter(data io.ReadCloser, stream quic.SendStream) error {
	conn.lastFilePos = 0
	bytes, err := io.Copy(stream, data)
	if err != nil {
		stream.Close()
		return err
	}
	message := "Closing data stream, sent " + strconv.Itoa(int(bytes)) + " bytes"
	conn.writeMessage(226, message)
	stream.Close()

	return nil
}*/

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

func (conn *Conn) getNewSendDataStream() (quic.SendStream, error) {
	conn.structAccessMutex.Lock()
	defer conn.structAccessMutex.Unlock()
	stream, err := conn.session.OpenUniStreamSync()
	if err != nil {
		return nil, err
	}
	return stream, nil
}

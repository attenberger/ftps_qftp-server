package ftpq

import (
	"bufio"
	"fmt"
	server "github.com/attenberger/ftps_qftp-server"
	"github.com/lucas-clemente/quic-go"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

type SubConn struct {
	connection    *Conn
	controlStream quic.Stream
	controlReader *bufio.Reader
	controlWriter *bufio.Writer
	logger        server.Logger
	driver        server.Driver
	sessionID     string
	reqUser       string
	user          string
	renameFrom    string
	lastFilePos   int64
	appendData    bool
	closed        bool
	namePrefix    string
}

func (subConn *SubConn) Serve() {
	defer subConn.connection.ReportSubConnFinsihed()
	// read commands
	for {
		line, err := subConn.controlReader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				subConn.logger.Print(subConn.sessionID+":"+strconv.FormatUint(uint64(subConn.controlStream.StreamID()), 10), fmt.Sprint("read error:", err))
			}

			break
		}
		subConn.receiveLine(line)
		// QUIT command closes connection, break to avoid error on reading from
		// closed socket
		if subConn.closed == true {
			break
		}
	}
	subConn.Close()
	subConn.logger.Print(subConn.sessionID+":"+strconv.FormatUint(uint64(subConn.controlStream.StreamID()), 10), "Stream Terminated")
}

func (subConn *SubConn) LoginUser() string {
	return subConn.user
}

func (subConn *SubConn) IsLogin() bool {
	return len(subConn.user) > 0
}

// Close will manually close this connection, even if the client isn't ready.
func (subConn *SubConn) Close() {
	subConn.controlStream.Close()
	subConn.closed = true
}

// writeMessage will send a standard FTP response back to the client.
func (subConn *SubConn) writeMessage(code int, message string) (wrote int, err error) {
	subConn.logger.PrintResponse(subConn.sessionID+":"+strconv.FormatUint(uint64(subConn.controlStream.StreamID()), 10), code, message)
	line := fmt.Sprintf("%d %s\r\n", code, message)
	wrote, err = subConn.controlWriter.WriteString(line)
	subConn.controlWriter.Flush()
	return
}

// writeMessage will send a standard FTP response back to the client.
func (subConn *SubConn) writeMessageMultiline(code int, message string) (wrote int, err error) {
	subConn.logger.PrintResponse(subConn.sessionID+":"+strconv.FormatUint(uint64(subConn.controlStream.StreamID()), 10), code, message)
	line := fmt.Sprintf("%d-%s\r\n%d END\r\n", code, message, code)
	wrote, err = subConn.controlWriter.WriteString(line)
	subConn.controlWriter.Flush()
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
func (subConn *SubConn) buildPath(filename string) (fullPath string) {
	if len(filename) > 0 && filename[0:1] == "/" {
		fullPath = filepath.Clean(filename)
	} else if len(filename) > 0 && filename != "-a" {
		fullPath = filepath.Clean(subConn.namePrefix + "/" + filename)
	} else {
		fullPath = filepath.Clean(subConn.namePrefix)
	}
	fullPath = strings.Replace(fullPath, "//", "/", -1)
	fullPath = strings.Replace(fullPath, string(filepath.Separator), "/", -1)
	return
}

// receiveLine accepts a single line FTP command and co-ordinates an
// appropriate response.
func (subConn *SubConn) receiveLine(line string) {
	command, param := subConn.parseLine(line)
	subConn.logger.PrintCommand(subConn.sessionID+":"+strconv.FormatUint(uint64(subConn.controlStream.StreamID()), 10), command, param)
	cmdObj := commands[strings.ToUpper(command)]
	if cmdObj == nil {
		subConn.writeMessage(502, "Command not found")
		return
	}
	if cmdObj.RequireParam() && param == "" {
		subConn.writeMessage(553, "action aborted, required param missing")
	} else if cmdObj.RequireAuth() && subConn.user == "" {
		subConn.writeMessage(530, "not logged in")
	} else {
		cmdObj.Execute(subConn, param)
	}
}

func (subConn *SubConn) parseLine(line string) (string, string) {
	params := strings.SplitN(strings.Trim(line, "\r\n"), " ", 2)
	if len(params) == 1 {
		return params[0], ""
	}
	return params[0], strings.TrimSpace(params[1])
}

// sendOutofbandData will send a string to the client via the currently open
// data socket. Assumes the socket is open and ready to be used.
func (subConn *SubConn) sendOutofbandData(data []byte, stream quic.SendStream) quic.StreamID {
	bytes := len(data)
	stream.Write(data)
	streamID := stream.StreamID()
	stream.Close()
	message := "Closing data strea,, sent " + strconv.Itoa(bytes) + " bytes"
	subConn.writeMessage(226, message)

	return streamID
}

func (subConn *SubConn) sendOutofBandDataWriter(data io.ReadCloser, stream quic.SendStream) error {
	subConn.lastFilePos = 0
	bytes, err := io.Copy(stream, data)
	if err != nil {
		stream.Close()
		return err
	}
	message := "Closing data stream, sent " + strconv.Itoa(int(bytes)) + " bytes"
	subConn.writeMessage(226, message)
	stream.Close()

	return nil
}

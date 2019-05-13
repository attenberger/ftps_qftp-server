// Copyright 2018 The goftp Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package server

import (
	"fmt"
	"github.com/attenberger/quic-go"
	"log"
	"strconv"
	"strings"
)

type Command interface {
	IsExtend() bool
	RequireParam() bool
	RequireAuth() bool
	Execute(*SubConn, string)
}

type commandMap map[string]Command

var (
	commands = commandMap{
		"ALLO": commandAllo{},
		"APPE": commandAppe{},
		"CDUP": commandCdup{},
		"CWD":  commandCwd{},
		"DELE": commandDele{},
		"FEAT": commandFeat{},
		"LIST": commandList{},
		"NLST": commandNlst{},
		"MDTM": commandMdtm{},
		"MKD":  commandMkd{},
		"MODE": commandMode{},
		"NOOP": commandNoop{},
		"OPTS": commandOpts{},
		"PASS": commandPass{},
		"PWD":  commandPwd{},
		"QUIT": commandQuit{},
		"RETR": commandRetr{},
		"REST": commandRest{},
		"RNFR": commandRnfr{},
		"RNTO": commandRnto{},
		"RMD":  commandRmd{},
		"SIZE": commandSize{},
		"STOR": commandStor{},
		"STRU": commandStru{},
		"SYST": commandSyst{},
		"TYPE": commandType{},
		"USER": commandUser{},
		"XCUP": commandCdup{},
		"XCWD": commandCwd{},
		"XPWD": commandPwd{},
		"XRMD": commandRmd{},
	}
)

// commandAllo responds to the ALLO FTP command.
//
// This is essentially a ping from the client so we just respond with an
// basic OK message.
type commandAllo struct{}

func (cmd commandAllo) IsExtend() bool {
	return false
}

func (cmd commandAllo) RequireParam() bool {
	return false
}

func (cmd commandAllo) RequireAuth() bool {
	return false
}

func (cmd commandAllo) Execute(subConn *SubConn, param string) {
	subConn.writeMessage(202, "Obsolete")
}

type commandAppe struct{}

func (cmd commandAppe) IsExtend() bool {
	return false
}

func (cmd commandAppe) RequireParam() bool {
	return false
}

func (cmd commandAppe) RequireAuth() bool {
	return true
}

func (cmd commandAppe) Execute(subConn *SubConn, param string) {
	subConn.appendData = true
	subConn.writeMessage(202, "Obsolete")
}

type commandOpts struct{}

func (cmd commandOpts) IsExtend() bool {
	return false
}

func (cmd commandOpts) RequireParam() bool {
	return false
}

func (cmd commandOpts) RequireAuth() bool {
	return false
}

func (cmd commandOpts) Execute(subConn *SubConn, param string) {
	parts := strings.Fields(param)
	if len(parts) != 2 {
		subConn.writeMessage(550, "Unknow params")
		return
	}
	if strings.ToUpper(parts[0]) != "UTF8" {
		subConn.writeMessage(550, "Unknow params")
		return
	}

	if strings.ToUpper(parts[1]) == "ON" {
		subConn.writeMessage(200, "UTF8 mode enabled")
	} else {
		subConn.writeMessage(550, "Unsupported non-utf8 mode")
	}
}

type commandFeat struct{}

func (cmd commandFeat) IsExtend() bool {
	return false
}

func (cmd commandFeat) RequireParam() bool {
	return false
}

func (cmd commandFeat) RequireAuth() bool {
	return false
}

var (
	feats    = "Extensions supported:\n%s"
	featCmds = " UTF8\n"
)

func init() {
	for k, v := range commands {
		if v.IsExtend() {
			featCmds = featCmds + " " + k + "\n"
		}
	}
}

func (cmd commandFeat) Execute(subConn *SubConn, param string) {
	subConn.writeMessageMultiline(211, subConn.connection.server.feats)
}

// cmdCdup responds to the CDUP FTP command.
//
// Allows the client change their current directory to the parent.
type commandCdup struct{}

func (cmd commandCdup) IsExtend() bool {
	return false
}

func (cmd commandCdup) RequireParam() bool {
	return false
}

func (cmd commandCdup) RequireAuth() bool {
	return true
}

func (cmd commandCdup) Execute(subConn *SubConn, param string) {
	otherCmd := &commandCwd{}
	otherCmd.Execute(subConn, "..")
}

// commandCwd responds to the CWD FTP command. It allows the client to change the
// current working directory.
type commandCwd struct{}

func (cmd commandCwd) IsExtend() bool {
	return false
}

func (cmd commandCwd) RequireParam() bool {
	return true
}

func (cmd commandCwd) RequireAuth() bool {
	return true
}

func (cmd commandCwd) Execute(subConn *SubConn, param string) {
	path := subConn.buildPath(param)
	err := subConn.driver.ChangeDir(path)
	if err == nil {
		subConn.namePrefix = path
		subConn.writeMessage(250, "Directory changed to "+path)
	} else {
		subConn.writeMessage(550, fmt.Sprint("Directory change to ", path, " failed: ", err))
	}
}

// commandDele responds to the DELE FTP command. It allows the client to delete
// a file
type commandDele struct{}

func (cmd commandDele) IsExtend() bool {
	return false
}

func (cmd commandDele) RequireParam() bool {
	return true
}

func (cmd commandDele) RequireAuth() bool {
	return true
}

func (cmd commandDele) Execute(subConn *SubConn, param string) {
	path := subConn.buildPath(param)
	err := subConn.driver.DeleteFile(path)
	if err == nil {
		subConn.writeMessage(250, "File deleted")
	} else {
		subConn.writeMessage(550, fmt.Sprint("File delete failed: ", err))
	}
}

// commandList responds to the LIST FTP command. It allows the client to retreive
// a detailed listing of the contents of a directory.
type commandList struct{}

func (cmd commandList) IsExtend() bool {
	return false
}

func (cmd commandList) RequireParam() bool {
	return false
}

func (cmd commandList) RequireAuth() bool {
	return true
}

func (cmd commandList) Execute(subConn *SubConn, param string) {
	path := subConn.buildPath(parseListParam(param))
	info, err := subConn.driver.Stat(path)
	if err != nil {
		subConn.writeMessage(550, err.Error())
		return
	}

	if info == nil {
		subConn.logger.Printf(subConn.sessionID+":"+strconv.FormatUint(uint64(subConn.controlStream.StreamID()), 10), "%s: no such file or directory.\n", path)
		return
	}
	var files []FileInfo
	if info.IsDir() {
		err = subConn.driver.ListDir(path, func(f FileInfo) error {
			files = append(files, f)
			return nil
		})
		if err != nil {
			subConn.writeMessage(550, err.Error())
			return
		}
	} else {
		files = append(files, info)
	}
	stream, err := subConn.connection.getNewSendDataStream()
	if err != nil {
		subConn.writeMessage(425, "Can't open data stream.")
		return
	}
	subConn.writeMessage(150, fmt.Sprintf("%d Opening ASCII mode data connection for file list", stream.StreamID()))
	subConn.sendOutofbandData(listFormatter(files).Detailed(), stream)
}

func parseListParam(param string) (path string) {
	if len(param) == 0 {
		path = param
	} else {
		fields := strings.Fields(param)
		i := 0
		for _, field := range fields {
			if !strings.HasPrefix(field, "-") {
				break
			}
			i = strings.LastIndex(param, " "+field) + len(field) + 1
		}
		path = strings.TrimLeft(param[i:], " ") //Get all the path even with space inside
	}
	return path
}

// commandNlst responds to the NLST FTP command. It allows the client to
// retreive a list of filenames in the current directory.
type commandNlst struct{}

func (cmd commandNlst) IsExtend() bool {
	return false
}

func (cmd commandNlst) RequireParam() bool {
	return false
}

func (cmd commandNlst) RequireAuth() bool {
	return true
}

func (cmd commandNlst) Execute(subConn *SubConn, param string) {
	path := subConn.buildPath(parseListParam(param))
	info, err := subConn.driver.Stat(path)
	if err != nil {
		subConn.writeMessage(550, err.Error())
		return
	}
	if !info.IsDir() {
		subConn.writeMessage(550, param+" is not a directory")
		return
	}

	var files []FileInfo
	err = subConn.driver.ListDir(path, func(f FileInfo) error {
		files = append(files, f)
		return nil
	})
	if err != nil {
		subConn.writeMessage(550, err.Error())
		return
	}
	stream, err := subConn.connection.getNewSendDataStream()
	if err != nil {
		subConn.writeMessage(425, "Can't open data stream.")
		return
	}
	subConn.writeMessage(150, fmt.Sprintf("%d Opening ASCII mode data connection for file list", stream.StreamID()))
	subConn.sendOutofbandData(listFormatter(files).Short(), stream)
}

// commandMdtm responds to the MDTM FTP command. It allows the client to
// retreive the last modified time of a file.
type commandMdtm struct{}

func (cmd commandMdtm) IsExtend() bool {
	return false
}

func (cmd commandMdtm) RequireParam() bool {
	return true
}

func (cmd commandMdtm) RequireAuth() bool {
	return true
}

func (cmd commandMdtm) Execute(subConn *SubConn, param string) {
	path := subConn.buildPath(param)
	stat, err := subConn.driver.Stat(path)
	if err == nil {
		subConn.writeMessage(213, stat.ModTime().Format("20060102150405"))
	} else {
		subConn.writeMessage(450, "File not available")
	}
}

// commandMkd responds to the MKD FTP command. It allows the client to create
// a new directory
type commandMkd struct{}

func (cmd commandMkd) IsExtend() bool {
	return false
}

func (cmd commandMkd) RequireParam() bool {
	return true
}

func (cmd commandMkd) RequireAuth() bool {
	return true
}

func (cmd commandMkd) Execute(subConn *SubConn, param string) {
	path := subConn.buildPath(param)
	err := subConn.driver.MakeDir(path)
	if err == nil {
		subConn.writeMessage(257, "Directory created")
	} else {
		subConn.writeMessage(550, fmt.Sprint("Action not taken: ", err))
	}
}

// cmdMode responds to the MODE FTP command.
//
// the original FTP spec had various options for hosts to negotiate how data
// would be sent over the data socket, In reality these days (S)tream mode
// is all that is used for the mode - data is just streamed down the data
// socket unchanged.
type commandMode struct{}

func (cmd commandMode) IsExtend() bool {
	return false
}

func (cmd commandMode) RequireParam() bool {
	return true
}

func (cmd commandMode) RequireAuth() bool {
	return true
}

func (cmd commandMode) Execute(subConn *SubConn, param string) {
	if strings.ToUpper(param) == "S" {
		subConn.writeMessage(200, "OK")
	} else {
		subConn.writeMessage(504, "MODE is an obsolete command")
	}
}

// cmdNoop responds to the NOOP FTP command.
//
// This is essentially a ping from the client so we just respond with an
// basic 200 message.
type commandNoop struct{}

func (cmd commandNoop) IsExtend() bool {
	return false
}

func (cmd commandNoop) RequireParam() bool {
	return false
}

func (cmd commandNoop) RequireAuth() bool {
	return false
}

func (cmd commandNoop) Execute(subConn *SubConn, param string) {
	subConn.writeMessage(200, "OK")
}

// commandPass respond to the PASS FTP command by asking the driver if the
// supplied username and password are valid
type commandPass struct{}

func (cmd commandPass) IsExtend() bool {
	return false
}

func (cmd commandPass) RequireParam() bool {
	return true
}

func (cmd commandPass) RequireAuth() bool {
	return false
}

func (cmd commandPass) Execute(subConn *SubConn, param string) {
	ok, err := subConn.connection.server.Auth.CheckPasswd(subConn.reqUser, param)
	if err != nil {
		subConn.writeMessage(550, "Checking password error")
		return
	}

	if ok {
		subConn.user = subConn.reqUser
		subConn.reqUser = ""
		subConn.writeMessage(230, "Password ok, continue")
	} else {
		subConn.writeMessage(530, "Incorrect password, not logged in")
	}
}

// commandPwd responds to the PWD FTP command.
//
// Tells the client what the current working directory is.
type commandPwd struct{}

func (cmd commandPwd) IsExtend() bool {
	return false
}

func (cmd commandPwd) RequireParam() bool {
	return false
}

func (cmd commandPwd) RequireAuth() bool {
	return true
}

func (cmd commandPwd) Execute(subConn *SubConn, param string) {
	subConn.writeMessage(257, "\""+subConn.namePrefix+"\" is the current directory")
}

// CommandQuit responds to the QUIT FTP command. The client has requested the
// connection be closed.
type commandQuit struct{}

func (cmd commandQuit) IsExtend() bool {
	return false
}

func (cmd commandQuit) RequireParam() bool {
	return false
}

func (cmd commandQuit) RequireAuth() bool {
	return false
}

func (cmd commandQuit) Execute(subConn *SubConn, param string) {
	subConn.writeMessage(221, "Goodbye")
	subConn.Close()
}

// commandRetr responds to the RETR FTP command. It allows the client to
// download a file.
type commandRetr struct{}

func (cmd commandRetr) IsExtend() bool {
	return false
}

func (cmd commandRetr) RequireParam() bool {
	return true
}

func (cmd commandRetr) RequireAuth() bool {
	return true
}

func (cmd commandRetr) Execute(subConn *SubConn, param string) {
	path := subConn.buildPath(param)
	defer func() {
		subConn.lastFilePos = 0
		subConn.appendData = false
	}()
	bytes, data, err := subConn.driver.GetFile(path, subConn.lastFilePos)
	if err == nil {
		defer data.Close()
		stream, err := subConn.connection.getNewSendDataStream()
		if err != nil {
			subConn.writeMessage(425, "Can't open data stream.")
			return
		}
		subConn.writeMessage(150, fmt.Sprintf("%d Data transfer starting %v bytes", stream.StreamID(), bytes))
		err = subConn.sendOutofBandDataWriter(data, stream)
		if err != nil {
			subConn.writeMessage(551, "Error reading file")
		}
	} else {
		subConn.writeMessage(551, "File not available")
	}
}

type commandRest struct{}

func (cmd commandRest) IsExtend() bool {
	return false
}

func (cmd commandRest) RequireParam() bool {
	return true
}

func (cmd commandRest) RequireAuth() bool {
	return true
}

func (cmd commandRest) Execute(subConn *SubConn, param string) {
	var err error
	subConn.lastFilePos, err = strconv.ParseInt(param, 10, 64)
	if err != nil {
		subConn.writeMessage(551, "File not available")
		return
	}

	subConn.appendData = true

	subConn.writeMessage(350, fmt.Sprint("Start transfer from ", subConn.lastFilePos))
}

// commandRnfr responds to the RNFR FTP command. It's the first of two commands
// required for a client to rename a file.
type commandRnfr struct{}

func (cmd commandRnfr) IsExtend() bool {
	return false
}

func (cmd commandRnfr) RequireParam() bool {
	return true
}

func (cmd commandRnfr) RequireAuth() bool {
	return true
}

func (cmd commandRnfr) Execute(subConn *SubConn, param string) {
	subConn.renameFrom = subConn.buildPath(param)
	subConn.writeMessage(350, "Requested file action pending further information.")
}

// cmdRnto responds to the RNTO FTP command. It's the second of two commands
// required for a client to rename a file.
type commandRnto struct{}

func (cmd commandRnto) IsExtend() bool {
	return false
}

func (cmd commandRnto) RequireParam() bool {
	return true
}

func (cmd commandRnto) RequireAuth() bool {
	return true
}

func (cmd commandRnto) Execute(subConn *SubConn, param string) {
	toPath := subConn.buildPath(param)
	err := subConn.driver.Rename(subConn.renameFrom, toPath)
	defer func() {
		subConn.renameFrom = ""
	}()

	if err == nil {
		subConn.writeMessage(250, "File renamed")
	} else {
		subConn.writeMessage(550, fmt.Sprint("Action not taken: ", err))
	}
}

// cmdRmd responds to the RMD FTP command. It allows the client to delete a
// directory.
type commandRmd struct{}

func (cmd commandRmd) IsExtend() bool {
	return false
}

func (cmd commandRmd) RequireParam() bool {
	return true
}

func (cmd commandRmd) RequireAuth() bool {
	return true
}

func (cmd commandRmd) Execute(subConn *SubConn, param string) {
	path := subConn.buildPath(param)
	err := subConn.driver.DeleteDir(path)
	if err == nil {
		subConn.writeMessage(250, "Directory deleted")
	} else {
		subConn.writeMessage(550, fmt.Sprint("Directory delete failed: ", err))
	}
}

// commandSize responds to the SIZE FTP command. It returns the size of the
// requested path in bytes.
type commandSize struct{}

func (cmd commandSize) IsExtend() bool {
	return false
}

func (cmd commandSize) RequireParam() bool {
	return true
}

func (cmd commandSize) RequireAuth() bool {
	return true
}

func (cmd commandSize) Execute(subConn *SubConn, param string) {
	path := subConn.buildPath(param)
	stat, err := subConn.driver.Stat(path)
	if err != nil {
		log.Printf("Size: error(%s)", err)
		subConn.writeMessage(450, fmt.Sprint("path", path, "not found"))
	} else {
		subConn.writeMessage(213, strconv.Itoa(int(stat.Size())))
	}
}

// commandStor responds to the STOR FTP command. It allows the user to upload a
// new file.
type commandStor struct{}

func (cmd commandStor) IsExtend() bool {
	return false
}

func (cmd commandStor) RequireParam() bool {
	return true
}

func (cmd commandStor) RequireAuth() bool {
	return true
}

func (cmd commandStor) Execute(subConn *SubConn, param string) {
	params := strings.SplitN(param, " ", 2)
	if len(params) != 2 {
		subConn.writeMessage(501, "Stream ID and path seperated by a blank needed.")
	}
	streamIDUint64, err := strconv.ParseInt(params[0], 10, 64)
	if err != nil || streamIDUint64 < 0 || streamIDUint64%4 != 2 {
		subConn.writeMessage(501, "Stream ID has not a valid value for a unidirectional stream from the client.")
	}
	streamID := quic.StreamID(streamIDUint64)
	subConn.writeMessage(150, "Data transfer starting")
	stream, err := subConn.connection.getReceiveDataStream(streamID)
	if err != nil {
		subConn.writeMessage(425, "Can't open data stream.")
	}

	targetPath := subConn.buildPath(params[1])

	defer func() {
		subConn.appendData = false
	}()

	bytes, err := subConn.driver.PutFile(targetPath, stream, subConn.appendData)
	if err == nil {
		msg := "OK, received " + strconv.Itoa(int(bytes)) + " bytes"
		subConn.writeMessage(226, msg)
	} else {
		subConn.writeMessage(450, fmt.Sprint("error during transfer: ", err))
	}
}

// commandStru responds to the STRU FTP command.
//
// like the MODE and TYPE commands, stru[cture] dates back to a time when the
// FTP protocol was more aware of the content of the files it was transferring,
// and would sometimes be expected to translate things like EOL markers on the
// fly.
//
// These days files are sent unmodified, and F(ile) mode is the only one we
// really need to support.
type commandStru struct{}

func (cmd commandStru) IsExtend() bool {
	return false
}

func (cmd commandStru) RequireParam() bool {
	return true
}

func (cmd commandStru) RequireAuth() bool {
	return true
}

func (cmd commandStru) Execute(subConn *SubConn, param string) {
	if strings.ToUpper(param) == "F" {
		subConn.writeMessage(200, "OK")
	} else {
		subConn.writeMessage(504, "STRU is an obsolete command")
	}
}

// commandSyst responds to the SYST FTP command by providing a canned response.
type commandSyst struct{}

func (cmd commandSyst) IsExtend() bool {
	return false
}

func (cmd commandSyst) RequireParam() bool {
	return false
}

func (cmd commandSyst) RequireAuth() bool {
	return true
}

func (cmd commandSyst) Execute(subConn *SubConn, param string) {
	subConn.writeMessage(215, "UNIX Type: L8")
}

// commandType responds to the TYPE FTP command.
//
//  like the MODE and STRU commands, TYPE dates back to a time when the FTP
//  protocol was more aware of the content of the files it was transferring, and
//  would sometimes be expected to translate things like EOL markers on the fly.
//
//  Valid options were A(SCII), I(mage), E(BCDIC) or LN (for local type). Since
//  we plan to just accept bytes from the client unchanged, I think Image mode is
//  adequate. The RFC requires we accept ASCII mode however, so accept it, but
//  ignore it.
type commandType struct{}

func (cmd commandType) IsExtend() bool {
	return false
}

func (cmd commandType) RequireParam() bool {
	return false
}

func (cmd commandType) RequireAuth() bool {
	return true
}

func (cmd commandType) Execute(subConn *SubConn, param string) {
	if strings.ToUpper(param) == "A" {
		subConn.writeMessage(200, "Type set to ASCII")
	} else if strings.ToUpper(param) == "I" {
		subConn.writeMessage(200, "Type set to binary")
	} else {
		subConn.writeMessage(500, "Invalid type")
	}
}

// commandUser responds to the USER FTP command by asking for the password
type commandUser struct{}

func (cmd commandUser) IsExtend() bool {
	return false
}

func (cmd commandUser) RequireParam() bool {
	return true
}

func (cmd commandUser) RequireAuth() bool {
	return false
}

func (cmd commandUser) Execute(subConn *SubConn, param string) {
	subConn.reqUser = param
	subConn.writeMessage(331, "User name ok, password required")
}

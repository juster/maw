/* 	ipc.go

	When maw is invoked runs as a master or slave process.
	
	The master process is what is activated by default, by the user. This starts the master and
	at least one slave process. The slave processes are spawned by makepkg. Maw calls makepkg,
	specifying PACMAN=maw in order to have makepkg use maw -Ss for syncing AUR deps that
	it needs. The child processes communicate back to the original server process by using a file
	descriptor that is passed through makepkg.
	
	This keeps all user interaction in the master. This is the original spawned process.
	This also prevents multiple versions of pacman trying to install at the same time... and failing
	horribly. The only process installing packages is the original parent process.
	
	Packages are built by child processes. Because they are not installing packages their
	priviledges can also be dropped immediately to a non-root user.
*/

package main

import (
	"io"
	"os"
	"fmt"
	"bufio"
	"strconv"
	"regexp"
)

const (
	MawPipeFd = 3
)

var (
	msgMatch = regexp.MustCompile("^([0-9]+):([^:]+):([^:]+):(.*)$")
)

// Messages can only be sent from slave processes to the master process.
type Message struct {
	Pid int
	Key string
	Action string
	Param string
}

type MessageReader struct {
	linerdr *bufio.Reader
}

func NewMessageReader(reader io.Reader) (*MessageReader) {
	linerdr := bufio.NewReader(reader)
	return &MessageReader{linerdr}
}

func (mmr *MessageReader) ReadNext() (*Message, os.Error) {
	buff, prefix, err := mmr.linerdr.ReadLine()
	switch {
	case err != nil:
		return nil, err
	case prefix:
		return nil, os.NewError("Line buffer read an extremely long line, possibly corrupt message.")
	}

	line := string(buff)
	matches := msgMatch.FindStringSubmatch(line)
	if matches == nil {
		return nil, os.NewError("Failed to parse IPC message: " + string(line))
	}
	if L := len(matches); L == 0 || L > 3 {
		return nil, os.NewError("Failed to parse IPC message: " + string(line))
	}

	pid, err := strconv.Atoi(matches[0])
	if err != nil {
		return nil, err
	}

	return &Message{pid, matches[1], matches[2], matches[3]}, nil
}

type MessageWriter struct {
	pid int
	secret string
	pipe *os.File
}

func NewMessageWriter(secret string) (*MessageWriter) {
	file := os.NewFile(MawPipeFd, "maw-pipe")
	return &MessageWriter{os.Getpid(), secret, file}
}

func (writer *MessageWriter) SendMessage(command, arg string) os.Error {
	_, err := writer.pipe.WriteString(fmt.Sprintf("%d:%s:%s:%s\n",
		writer.pid, writer.secret, command, arg))
	return err
}

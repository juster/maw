/* 	ipc.go

	When maw is invoked it is either invoked as a parent ("server") or child ("client") process.
	
	The server process is what is activated by default, by the user. This starts the server and
	at least one client process. The client processes are spawned my maw itself. Or actually,
	spawned by makepkg. Maw calls makepkg, specifying PACMAN=maw in order to have
	makepkg use maw -Ss for syncing AUR deps that it needs. The child processes communicate
	back to the original server process by using a file descriptor that is passed through makepkg.
	
	This keeps all user interaction in the "server" or "parent". This is the original spawned process.
	This also prevents multiple versions of pacman trying to install at the same time... and failing
	horribly. The only process installing packages is the original parent process.
	
	Packages are built by child processes. Because they are not installing packages their
	priviledges can also be dropped immediately to a non-root user.
*/

package main

import (
	"io"
	"os"
	"os/signal"
	"fmt"
	"bufio"
	"strings"
	"strconv"
	"syscall"
	"regexp"
)

const (
	MawPipeFd = 3
)

var (
	msgMatch = regexp.MustCompile("^([0-9]+):([^:]+):(.*)$")
)

// Messages can only be sent from child to parent.
type Message struct {
	ChildPid int
	Action string
	Params []string
}

// NewMessage gets a new message ready to send. Should only be used by child processes.
func NewMessage(act string, params ... string) (*Message) {
	pid := os.Getpid()
	return &Message{pid, act, params}
}

func (msg *Message) String() (string) {
	act := strings.ToUpper(msg.Action)
	return fmt.Sprintf("%d:%s:%s\n", msg.ChildPid, act, msg.Params)
}

type MessageReader struct {
	linerdr *bufio.Reader
}

func NewMessageReader(reader io.Reader) (*MessageReader) {
	linerdr := bufio.NewReader(reader)
	return &MessageReader{linerdr}
}

func (mmr *MessageReader) ReadNext() (*Message, os.Error) {
	line, prefix, err := mmr.linerdr.ReadLine()
	switch {
	case err != nil:
		return nil, err
	case prefix:
		return nil, os.NewError("Line buffer read an extremely long line, possibly corrupt message.")
	}

	matches := msgMatch.FindSubmatch(line)
	if matches == nil {
		return nil, os.NewError("Failed to parse IPC message: " + string(line))
	}
	if L := len(matches); L == 0 || L > 2 {
		return nil, os.NewError("Failed to parse IPC message: " + string(line))
	}
	
	var pid int
	var act string
	var params []string
	
	for i, elem := range matches {
		str := string(elem)
		
		switch i {
		case 0: pid, _ = strconv.Atoi(str)
		case 1: act = str
		case 2: params = strings.Split(str, " ", -1)
		}
	}
	
	return &Message{pid, act, params}, nil
}

type MessageRPC func (int, ... string)

type InstallProc struct {
	childrenIds []int
	msgReader *MessageReader
	dispatchTable map[string] MessageRPC
}

// NewInstallProc returns a new InstallProc and the writer end of the pipe to pass to children.
func NewInstallProc() (*InstallProc, *os.File, os.Error) {
	readpipe, writepipe, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	reader := NewMessageReader(readpipe)
	
	parent := &InstallProc{make([]int, 8, 64), reader, nil}
	dispatch := map[string]MessageRPC{"HAI": func (pid int, _ ... string) { parent.addChild(pid) },
		"BAI": func (pid int, _ ... string) { parent.remChild(pid) }}
	parent.dispatchTable = dispatch
	return parent, writepipe, nil
}

func (proc *InstallProc) StartListening() {
	msgchan := make(chan *Message, 32)
	go proc.MessageLoop(msgchan)
	
	var msg *Message
	var sig signal.Signal
ListenLoop:
	select {
	case msg = <- msgchan:
		f := proc.dispatchTable[msg.Action]
		if f != nil {
			f(msg.ChildPid)
		}
	case sig = <- signal.Incoming:
		switch sig.(signal.UnixSignal) {
		// TODO: make constants
		case 1: fallthrough
		case 2: fallthrough
		case 5: 
			proc.killChildren(9)
			break ListenLoop
		}
	}
}

func (proc *InstallProc) MessageLoop(c chan *Message) {
	for {
		msg, err := proc.msgReader.ReadNext()
		if err != nil {
			panic(err.String())
		}
		c <- msg
	}
}

// Start keeping track of a child process and responding to its messages.
func (proc *InstallProc) addChild(pid int, unused ... string) {
	for _, oldPid := range proc.childrenIds {
		if oldPid == pid {
			return
		}
	}
	proc.childrenIds = append(proc.childrenIds, pid)
}

// Stop keeping track of a child process. Probably because it exited.
func (proc *InstallProc) remChild(pid int, unused ... string) (os.Error) {
	for i, curPid := range proc.childrenIds {
		if curPid == pid {
			proc.childrenIds = append(proc.childrenIds[0:i], proc.childrenIds[i+1:] ...)
			return nil
		}
	}
	return os.NewError(fmt.Sprintf("Could not remove child PID, it is not register: %d", pid))
}

// KillChildren is a gruesome name isn't it?
// This is the main reason to keep track of active children. Wait what?
func (proc *InstallProc) killChildren(sig int) {
	for _, pid := range proc.childrenIds {
		syscall.Kill(pid, sig) // ignores errors, we can't do anything about it
	}
}

type BuilderProc struct {
	msgWriter *os.File
}

func NewBuilderProc() (*BuilderProc) {
	file := os.NewFile(MawPipeFd, "maw-pipe")
	return &BuilderProc{file}
}

func (builder *BuilderProc) SendMessage(msg *Message) {
	builder.msgWriter.WriteString(msg.String())
}

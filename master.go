/*	master.go
	Master Process Functions
*/

package main

import (
	"exec"
	"syscall"
	"strings"
	"encoding/hex"
	"crypto/rand"
	"os/signal"
	"fmt"
	"os"
)

const (
	EventPacman = iota
	EventPrint = iota
	EventError = iota
	EventExit = iota

	SIGHUP = 1
	SIGINT = 2
	SIGUSR1 = 10
	SIGTERM = 15
	SIGUSR2 = 12
)

type UIEvent struct {
	SourcePid, Type int
	Param string
}

type SlaveSpawner interface {
	SpawnSlaveProcess(cmd []string, wdir string, outfile *os.File) (*os.Process, os.Error)
}

type MasterProc struct {
	key string
	slavePids []int
	msgReader *MessageReader
	readpipe, writepipe *os.File
	eventQueue chan *UIEvent
	packageFetchers []PackageFetcher
}

func generateKey() (string, os.Error) {
	randBytes := make([]byte, 0, 64)
	count, err := rand.Read(randBytes)
	if err != nil {
		return "", err
	}
	fmt.Printf("DBG: generated %d random bytes\n", count)

	secret := hex.EncodeToString(randBytes)
	fmt.Printf("DBG: generated key %s\n", secret)
	return secret, nil
}

// NewMasterProc creates a new MasterProc, generating a random key, a pipe to use
// for slave communication, and a MessageReader. If we cannot open a pipe,
// returns nil and an error.
func NewMasterProc() (*MasterProc, os.Error) {
	// Create our message pipes and message reader.
	readpipe, writepipe, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	reader := NewMessageReader(readpipe)

	// Generate a random key to pass to slaves.
	key, err := generateKey()
	if err != nil {
		return nil, err
	}

	// Define our event queue as a channel of events.
	eventQ := make(chan *UIEvent, 128)
	slavePids := make([]int, 0, 128)

	fetchers := []PackageFetcher{&PacmanFetcher{"."}, nil}
	master := &MasterProc{key, slavePids, reader, readpipe, writepipe, eventQ, fetchers}

	// The package builder needs a reference to the master proc for spawning slaves.
	builder := NewPackageBuilder(master)
	aurfetch := NewAURCache(".", ".", builder) // TODO: fancier srcdest and buildroot
	fetchers[1] = aurfetch

	return master, nil
}

func (proc *MasterProc) SpawnSlaveProcess(cmd []string, wdir string, outfile *os.File) (*os.Process, os.Error) {
	procpath := cmd[0]
	procargs := cmd[1:]

	// TODO: dup outfile file descriptors?
	procfiles := []*os.File{nil, outfile, outfile, proc.writepipe}
	procattr := &os.ProcAttr{wdir, nil, procfiles}

	return os.StartProcess(procpath, procargs, procattr)
}

func (proc *MasterProc) Start() {
	// Environment variables must be set before we spawn slave processes.
	os.Setenv(MAW_ENVVAR, proc.key)
	os.Setenv("PKGDEST", ".") // TODO: fancy this up

	// Messages should be evaluated all the time.
	go proc.messageLoop()

	var sig signal.Signal
	var evt *UIEvent

MainLoop:
	for {
		select {
		case evt = <- proc.eventQueue:
			// Events should not be run concurrently.
			proc.runEvent(evt)
		case sig = <- signal.Incoming:
			switch sig.(signal.UnixSignal) {
			// TODO: make constants
			case SIGHUP: fallthrough
			case SIGINT: fallthrough
			case SIGTERM:
				proc.killSlaves(SIGTERM)
				break MainLoop
			}
		}
	}
}

// Do what we gotta do with the message from the slave.
func (proc *MasterProc) messageLoop() {
	for {
		msg, err := proc.msgReader.ReadNext()
		if err != nil {
			panic(err.String())
		}

		if msg.Key != proc.key {
			fmt.Printf("DBG: Invalid key sent from %d\n", msg.Pid)
			continue
		}
		if ! proc.checkSlave(msg.Pid) {
			fmt.Printf("DBG: Unknown pid (%d) sent a message.\n", msg.Pid)
			continue
		}

		switch msg.Action {
		case "hello":
			proc.addSlave(msg.Pid)
		case "goodbye":
			proc.remSlave(msg.Pid)
			if len(proc.slavePids) == 0 {
				// Send a signal to ourselves to exit.
				syscall.Kill(os.Getpid(), SIGTERM)
				break
			}
		case "install":
			go proc.fetchAll(msg.Pid, msg.Param)
		case "remove":
			pacarg := "-Rd " + msg.Param
			proc.runPacman(msg.Pid, pacarg)
		}
		// Ignore unrecognized messages.
	}
}

// Start keeping track of a slave process and responding to its messages.
func (proc *MasterProc) addSlave(pid int) {
	for _, oldPid := range proc.slavePids {
		if oldPid == pid {
			return
		}
	}
	proc.slavePids = append(proc.slavePids, pid)
}

// Stop keeping track of a slave process. Probably because it exited.
func (proc *MasterProc) remSlave(pid int) os.Error {
	for i, curPid := range proc.slavePids {
		if curPid == pid {
			proc.slavePids = append(proc.slavePids[0:i], proc.slavePids[i+1:] ...)
			return nil
		}
	}
	return os.NewError(fmt.Sprintf("Could not remove child PID, it is not registered: %d", pid))
}

func (proc *MasterProc) checkSlave(pid int) bool {
	for _, slavePid := range proc.slavePids {
		if pid == slavePid {
			return true
		}
	}
	return false
}

func (proc *MasterProc) killSlaves(sig int) {
	for _, pid := range proc.slavePids {
		syscall.Kill(pid, sig) // ignores errors, we can't do anything about it
	}
}

// fetchAll fetches packages for the slave process with the fromPid process ID. packages is a
// string which contains all the package names we need to fetch, separated by spaces.
func (proc *MasterProc) fetchAll(fromPid int, packages string) {
	pkgnames := strings.Split(packages, " ", -1)

	// Packages are all fetched concurrently, independent of each other
	chans := make([]chan []string, len(pkgnames))
	for i, pkgname := range pkgnames {
		r := make(chan []string, 1)
		go proc.fetchPackage(pkgname, r)
		chans[i] = r
	}

	// Waits for all goroutines to finish, collecting results
	allpkgpaths := make([]string, 0, 256)
	for i, c := range chans {
		pkgpaths := <- c
		if pkgpaths == nil {
			// TODO: send the error as an event or signal
			panic("Could not find the "+pkgnames[i]+" package.")
		} else {
			allpkgpaths = append(allpkgpaths, pkgpaths ...)
		}
	}

	// Send all these package paths to a pacman install event.
	pacargs := "-U " + strings.Join(allpkgpaths, " ")
	proc.runPacman(fromPid, pacargs)
}

// fetchPackage tries to download or build the package file for the package named by pkgname.
// The resulting package files are sent over the results channel. Since multi-packages can
// build more than one package file, a slice of strings is sent over the channel.
func (proc *MasterProc) fetchPackage(pkgname string, results chan []string) {
	for _, fetcher := range proc.packageFetchers {
		pkgpaths, err := fetcher.Fetch(pkgname)
		if err != nil {
			fmt.Printf(err.String())
			continue
		} else if pkgpaths == nil {
			continue
		}

		results <- pkgpaths
		return
	}

	results <- nil
}

func (proc *MasterProc) runEvent(event *UIEvent) {
	switch event.Type {
	case EventPrint:
		fmt.Print(event.Param)
	case EventPacman:
		params := strings.Split(event.Param, " ", -1)
		args := make([]string, 1, len(params)+1)
		args[0] = "pacman"
		args = append(args, params ...)
		cmd, err := exec.Run("/usr/bin/pacman", args, nil, "",
			exec.PassThrough, exec.PassThrough, exec.PassThrough)
		if err != nil {
			panic(err.String())
		}

		waitstatus, err := cmd.Process.Wait(0)
		if err != nil {
			panic(err.String())
		}
		var success bool
		if code := waitstatus.ExitStatus(); code != 0 {
			success = false
		} else {
			success = true
		}
		proc.signalSlave(event.SourcePid, success)
	}
}

func (proc *MasterProc) Print(msg string) {
	// pid is not used when printing messages
	proc.eventQueue <- &UIEvent{0, EventPrint, msg}
}

func (proc *MasterProc) runPacman(pid int, cmdline string) {
	proc.eventQueue <- &UIEvent{pid, EventPacman, cmdline}
}

func (proc *MasterProc) signalSlave(pid int, success bool) {
	var sig int
	if success {
		sig = SIGUSR1
	} else {
		sig = SIGUSR2
	}
	syscall.Kill(pid, sig)
}

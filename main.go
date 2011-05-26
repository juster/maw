/* main.go
 * Makepkg Aur Wrapper - Program entrypoint
 * Justin Davis <jrcd83 at gmail>
 */

package main

import (
	"os"
	"fmt"
	"exec"
)

const (
	MAW_USERAGENT = "maw/1.0"
	MAW_ENVVAR = " MAWSECRET " // spaces are there so PKGBUILDs can't use it [easily]
	OptQuery = iota
	OptRemove = iota
	OptSync = iota
	OptDepTest = iota
	OptHelp = iota
)

type MawOpt struct {
	Action int
	Targets []string
}

func ParseOpts(cmdopts []string) *MawOpt {
	if len(cmdopts) == 0 {
		return &MawOpt{OptHelp, nil}
	}
	
	var act int
	switch cmdopts[0] {
	case "-Qq": act = OptQuery
	case "-Rns": act = OptRemove
	case "-S": act = OptSync
	case "-T": act = OptDepTest
	default: act = OptHelp
	}
	
	// Don't accidentally make flags into target packages,
	targets := make([]string, 0, len(cmdopts)-1)
	for _, opt := range cmdopts[1:] {
		if opt[0] != '-' {
			targets = append(targets, opt)
		}
	}
	
	return &MawOpt{act, targets}
}

func startMaster() int {
	master, err := NewMasterProc()
	if err != nil {
		fmt.Printf("Failed to start maw master: %s\n", err.String())
		return 1
	}

	// Start a slave process with our exact arguments now that a master process
	// is ready to receive its messages.
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		fmt.Printf("%s\n", err.String())
		return 1
	}
	_, err = master.SpawnSlaveProcess(os.Args, ".", devnull)
	if err != nil {
		fmt.Printf("Failed to respawn maw: %s\n", err.String())
		return 1
	}

	master.Start()
	return 0
}

func startSlave(opt *MawOpt, secret string) int {
	return 0
}

func runDepTest(opt *MawOpt) int {
	if len(opt.Targets) == 0 {
		return 0
	}
	args := make([]string, 0, len(opt.Targets)+2)
	args = append(args, []string{"pacman", "-T"} ...)
	args = append(args, opt.Targets ...)
	
	cmd, err := exec.Run("/usr/bin/pacman", args, nil, "",
		exec.DevNull, exec.PassThrough, exec.DevNull)
	if err != nil {
		goto DepTestError
	}

	status, err := cmd.Wait(0)
	if err != nil {
		goto DepTestError
	}
	return status.ExitStatus()

DepTestError:
	// Try printing an error even though we might be a slave process.
	fmt.Fprintf(os.Stderr, "Failed to run pacman: %s\n", err.String())
	return 1
}

func main() {
	opt := ParseOpts(os.Args[1:])

	// Handle easy operations where we don't have to worry about master/slave processes.
	switch opt.Action {
	case OptDepTest:
		os.Exit(runDepTest(opt))
	case OptHelp:
		fmt.Printf("Help help I'm being repressed!\nBloody peasants!\n")
		os.Exit(0)
	}

	var retval int
	mawsecret := os.Getenv(MAW_ENVVAR)
	if mawsecret == "" {
		// If the secret is not defined, maw has not yet started so we start a new
		// master process.
		retval = startMaster()
	} else {
		retval = startSlave(opt, mawsecret)
	}

	os.Exit(retval)
}

/*	pacman.go
	Pacman command-line parsing and functions for calling pacman.
*/

package main

import (
	"regexp"
	"strings"
)

const (
	ShortOpt = iota
	LongOpt = iota
	NotOpt = iota
	ParamedShortOpts = "bopr"
)

const (
	NotAction = iota
	SyncAction = iota
	OtherAction = iota
)

var (
	cmdOpt = regexp.MustCompile("^-([A-Za-z]+)|(-[a-z\\-]+)$")
	actionOpt = regexp.MustCompile(
	ParamedLongOpts = []string{"dbpath", "root", "arch", "cachedir", "config", "logfile",
		"owns", "file", "search", "print-format", "ignore", "ignoregroup"}
)

type PacmanOpt struct {
	Type int
	Action int
	Raw string
}

func ParsePacmanOpt(opt string) *PacmanOpt {
	optFlags := cmdOpt.FindStringSubmatch(opt)
	for i, match := range optFlags {
		switch i {
		case 0:
			if 
			return &PacmanOpt{ShortOpt, match}
		case 1: return &PacmanOpt{LongOpt, match}
		}
	}
	return &PacmanOpt{NotOpt, opt}
}

func (opt *PacmanOpt) Contains(char byte) bool {
	sub := string(char)
	return strings.Contains(opt.Raw, sub)
}

func (opt *PacmanOpt) FlagOn(char byte) bool {
	return opt.Type == ShortOpt && opt.Contains(char)
}

func (opt *PacmanOpt) IsSyncAction() bool {
	return opt.Contains('S')
}

func (opt *PacmanOpt) TakesParam() bool {
	switch opt.Type {
	case ShortOpt:
		// TODO: -l only takes arguments when used in syncing (aka -S)
		for _, ch := range ParamedShortOpts {
			if opt.Contains(byte(ch)) {
				return true
			}
		}
	case LongOpt:
		for _, popt := range ParamedLongOpts {
			if opt.Raw == popt {
				return true
			}
		}
	}
	return false
}

func ParseSyncTargets(cmdargs []string) []string {
	var sync_found, disable_sync bool
	targets := make([]string, 0, 128)
	
	for i := 0; i < len(cmdargs); i++ {
		opt := ParsePacmanOpt(cmdargs[i])
		
		// This argument is a package target, save it for later.
		if opt.Type == NotOpt {
			targets = append(targets, opt.Raw)
			continue
		}
		// We have found a sync request.
		if opt.FlagOn('S') {
			sync_found = true
		}
		// The search flag turns all targets into search queries.
		if opt.FlagOn('s') {
			disable_sync = true
		}
		if opt.TakesParam() {
			// The current option takes an argument, do not treat the next argument
			// as a target package name.
			i++
		}
	}
	
	if sync_found && ! disable_sync {
		return targets
	}
	return []string{}
}

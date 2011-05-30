/*	main.go
	Makepkg Aur Wrapper - Program entrypoint
	Justin Davis <jrcd83 at gmail>
*/

package main

import (
	"os"
	"os/user"
	"fmt"
	"exec"
)

const (
	MAW_USERAGENT = "maw/1.0"
	MAW_ENVVAR    = " MAWSECRET " // spaces are there so PKGBUILDs can't use it [easily]
	OptQuery      = iota
	OptRemove     = iota
	OptSync       = iota
	OptDepTest    = iota
	OptHelp       = iota
)

type MawOpt struct {
	Action  int
	AsDeps  bool
	Targets []string
}

/* This is used by other files, like in srcpkg.go and aur.go.
 * Kind of awkward placement but ohwell... */
func lookupSudoUser() *user.User {
	sudouser := os.Getenv("SUDO_USER")
	if sudouser == "" {
		return nil
	}
	userobj, _ := user.Lookup(sudouser)
	return userobj
}

func ParseOpts(cmdopts []string) *MawOpt {
	if len(cmdopts) == 0 {
		return &MawOpt{Action: OptHelp}
	}

	var act int
	var asdeps bool

	switch cmdopts[0] {
	case "-Qq":
		act = OptQuery
	case "-Rns":
		act = OptRemove
	case "-S":
		act = OptSync
	case "-T":
		act = OptDepTest
	default:
		act = OptHelp
	}

	// Don't accidentally make flags into target packages.
	targets := make([]string, 0, len(cmdopts)-1)
	for _, opt := range cmdopts[1:] {
		if opt[0] != '-' {
			targets = append(targets, opt)
		} else if opt == "--asdeps" {
			asdeps = true
		}
	}

	return &MawOpt{act, asdeps, targets}
}

func runPacman(flag string, args ...string) (int, os.Error) {
	procargs := make([]string, 2, len(args)+2)
	procargs[0] = "pacman"
	procargs[1] = flag
	procargs = append(procargs, args...)

	cmd, err := exec.Run("/usr/bin/pacman", procargs, nil, "",
		exec.PassThrough, exec.PassThrough, exec.PassThrough)
	if err != nil {
		return 0, err
	}

	waitstatus, err := cmd.Process.Wait(0)
	if err != nil {
		return 0, err
	}

	return waitstatus.ExitStatus(), nil
}

func runDepTest(opt *MawOpt) int {
	if len(opt.Targets) == 0 {
		return 0
	}

	code, err := runPacman("-T", opt.Targets...)
	if err != nil {
		fmt.Printf("error: %s\n", err.String())
		return 1
	}

	return code
}

////////////////////////////////////////////////////////////////////////////////
// SYNCING

func installPkgFiles(pkgpaths []string, asdeps bool) int {
	var args []string
	if asdeps {
		args = make([]string, 1, len(pkgpaths)+1)
		args[0] = "--asdeps"
		args = append(args, pkgpaths...)
	} else {
		args = pkgpaths
	}

	code, err := runPacman("-U", pkgpaths...)
	if err != nil {
		fmt.Printf("error: %s\n", err.String())
		return 1
	}

	return code
}

func runSyncInstall(opt *MawOpt) int {
	if len(opt.Targets) == 0 {
		fmt.Printf("error: no targets specified (use -h for help)\n")
		return 0
	}

	// Binary packages and source packages end up in /tmp
	os.Setenv("PKGDEST", "/tmp")

	builder := &PackageBuilder{}
	aurCache := NewAURCache("/tmp", ".", builder)
	multifetch := NewMultiFetcher(&PacmanFetcher{"/tmp"}, aurCache)

	pkgpaths, err := multifetch.FetchAll(opt.Targets)
	if err != nil {
		fmt.Printf("error: %s\n", err.String())
		return 1
	}

	return installPkgFiles(pkgpaths, opt.AsDeps)
}

func main() {
	opt := ParseOpts(os.Args[1:])

	switch opt.Action {
	case OptHelp:
		fmt.Printf("Help help I'm being repressed!\nBloody peasants!\n")
		os.Exit(0)
	case OptDepTest:
		retcode := runDepTest(opt)
		os.Exit(retcode)
	case OptSync:
		retcode := runSyncInstall(opt)
		os.Exit(retcode)
	}

	os.Exit(0)
}

/*	main.go
	Makepkg Aur Wrapper - Program entrypoint
	Justin Davis <jrcd83 at gmail>
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
	AsDeps bool
	Targets []string
}

func ParseOpts(cmdopts []string) *MawOpt {
	if len(cmdopts) == 0 {
		return &MawOpt{Action:OptHelp}
	}
	
	var act int
	var asdeps bool

	switch cmdopts[0] {
	case "-Qq": act = OptQuery
	case "-Rns": act = OptRemove
	case "-S": act = OptSync
	case "-T": act = OptDepTest
	default: act = OptHelp
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

func runPacman(flag string, args ... string) (int, os.Error) {
	procargs := make([]string, 2, len(args)+2)
	procargs[0] = "pacman"
	procargs[1] = flag
	procargs = append(procargs, args ...)

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

func fetchAllPackages(pkgnames []string, fetchers []PackageFetcher) ([]string, os.Error) {
	// Packages are all fetched concurrently, independent of each other
	chans := make([]chan []string, len(pkgnames))
	for i, pkgname := range pkgnames {
		r := make(chan []string, 1)
		go fetchPackage(pkgname, fetchers, r)
		chans[i] = r
	}

	// Waits for all goroutines to finish, collecting results
	allpkgpaths := make([]string, 0, 256) // TODO: use cap or something
	for i, c := range chans {
		pkgpaths := <- c
		if pkgpaths == nil {
			return nil, os.NewError("could not find " + pkgnames[i])
		} else {
			allpkgpaths = append(allpkgpaths, pkgpaths ...)
		}
	}

	return allpkgpaths, nil
}

// fetchPackage tries to download or build the package file for the package named by pkgname.
// The resulting package files are sent over the results channel. Since multi-packages can
// build more than one package file, a slice of strings is sent over the channel.
func fetchPackage(pkgname string, fetchers []PackageFetcher, results chan []string) {
	var pkgpaths []string

SearchLoop:
	for _, fetcher := range fetchers {
		var err FetchError
		pkgpaths, err = fetcher.Fetch(pkgname)
		if err != nil {
			if err.NotFound() {
				continue SearchLoop
			} else {
				fmt.Printf("error: %s\n", err.String())
				break SearchLoop
				// pkgpaths is now nil
			}
		}
	}

	results <- pkgpaths
}

func installPkgFiles(pkgpaths []string, asdeps bool) int {
	var args []string
	if asdeps {
		args = make([]string, 1, len(pkgpaths)+1)
		args[0] = "--asdeps"
		args = append(args, pkgpaths ...)
	} else {
		args = pkgpaths
	}

	code, err := runPacman("-U", pkgpaths ...)
	if err != nil {
		fmt.Printf("error: %s\n", err.String())
		return 1
	}

	return code
}

func runSyncInstall(opt *MawOpt) int {
	// Binary packages and source packages end up in /tmp
	os.Setenv("PKGDEST", "/tmp")
	
	builder := &PackageBuilder{}
	aurCache := NewAURCache("/tmp", ".", builder)
	fetchers := []PackageFetcher{&PacmanFetcher{"/tmp"}, aurCache}
	
	if len(opt.Targets) == 0 {
		fmt.Printf("error: no targets specified (use -h for help)\n")
	}
	
	pkgpaths, err := fetchAllPackages(opt.Targets, fetchers)
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

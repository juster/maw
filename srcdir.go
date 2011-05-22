package main

import (
	"os"
	"bufio"
	"io/ioutil"
)

type SrcDir struct {
	builddir string
}

func OpenSrcDir(path string) (*SrcDir, os.Error) {
	pathinfo, err := os.Stat(path)
	if err != nil {
		return nil, os.NewError("Failed to read source directory at " + path)
	}
	if ! pathinfo.IsDirectory() {
		return nil, os.NewError(path + " is not a valid source directory")
	}
	return &SrcDir{path}, nil
}

// Muahahaha!
// This creates the bash code we use to hook into makepkg. Makepkg
// will then print out the paths of the packages that it just created to
// the temporary filename we choose.
// Returns the bash code and temporary file name.
func bashHack() (string, *os.File, os.Error) {
	tmpfile, err := ioutil.TempFile("", "maw")
	if err != nil {
		return "", nil, err
	}

	bash := `
exit () {
  if [ "$1" -ne 0 ] ; then command exit $1 ; fi
  fullver=$(get_full_version $epoch $pkgver $pkgrel)
  for pkg in ${pkgname[@]} ; do
    for arch in "$CARCH" any ; do
      pkgfile="${PKGDEST}/${pkg}-${fullver}-${arch}${PKGEXT}"
      if [ -f "$pkgfile" ] ; then
        echo "$pkgfile" >>` + tmpfile.Name() + `
      fi
    done
  done
  command exit 0
}
source makepkg
`
	return bash, tmpfile, nil
}

// makepkg runs makepkg on the specified builddir. The resulting package is
// searched for in destdir. Notice we do not actually set PKGDEST ourselves, this
// should be done before calling this function.
// Returns the paths of built packages or nil and error if makepkg fails.
func (srcdir *SrcDir) makepkg() ([]string, os.Error) {
	// Chdir to builddir. Chdir back on func exit.
	olddir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	defer os.Chdir(olddir)
	err = os.Chdir(srcdir.builddir)
	if err != nil {
		return nil, err
	}

	bashcode, tmpfile, err := bashHack()
	if err != nil {
		return nil, err
	}
	defer func () {
		tmpname := tmpfile.Name()
		tmpfile.Close()
		os.Remove(tmpname)
	}()

	// We must force $0 to be makepkg... makepkg runs $0 internally.
	// Arguments after "-c" "..." override positional arguments $0, $1, ...
	args := []string{"bash", "-c", bashcode, "makepkg", "-m", "-f"}

	// Prepare to rock makepkg's world!
	outnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0666)
	if err != nil {
		return nil, err
	}
	errnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0666)
	if err != nil {
		return nil, err
	}
	files := []*os.File{nil, os.Stdout, os.Stderr}
	attr := &os.ProcAttr{"", nil, files}

	// Start it up and wait for it to finish.
	proc, err := os.StartProcess("/bin/bash", args, attr)
	if err != nil {
		return nil, err
	}
	status, err := proc.Wait(0)
	if err != nil {
		return nil, err
	}
	if code := status.ExitStatus(); code != 0 {
		return nil, os.NewError("makepkg failed")
	}
	outnull.Close()
	errnull.Close()
	proc.Release()

	// Read our sneaky tempfile. It contains the names of package files
	// that were built by makepkg.
	pkgpaths := make([]string, 0, 32)
	if err != nil {
		return nil, err
	}

	// Use bufio to read one line/path at a time.
	reader := bufio.NewReader(tmpfile)
RESULTLOOP:
	for {
		line, prefix, err := reader.ReadLine()
		switch {
		default:
			pkgpaths = append(pkgpaths, string(line))
		case prefix:
			return nil, os.NewError("Extremely long line for package path")
		case err == os.EOF:
			break RESULTLOOP
		case err != nil:
			return nil, err
		}
	}

	return pkgpaths, nil
}

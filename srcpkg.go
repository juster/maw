package main

import (
	"io"
	"os"
	"fmt"
	"time"
	"path"
	"path/filepath"
	"bufio"
	"strings"
	"syscall"
	"io/ioutil"
	"archive/tar"
	"compress/gzip"
)

type SrcPkg struct {
	path string
	file *os.File
	unzipper *gzip.Decompressor
	reader *tar.Reader
}

func OpenSrcPkg(path string) (*SrcPkg, os.Error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	
	unzipper, err := gzip.NewReader(file)
	if err != nil {
		file.Close()
		return nil, err
	}
	
	reader := tar.NewReader(unzipper)
	
	return &SrcPkg{path, file, unzipper, reader}, nil
}

func (srcpkg *SrcPkg) Close() {
	srcpkg.unzipper.Close()
	srcpkg.file.Close()
}

// PackageName extracts the name of the package from the path of the source package tarball.
func (srcpkg *SrcPkg) PackageName() (string, os.Error) {
	filename := path.Base(srcpkg.path)
	endidx := strings.Index(filename, ".")
	if endidx == -1 {
		return "", os.NewError("Invalid source package filename: " + filename)
	}
	pkgname := filename[0:endidx]
	return pkgname, nil
}

// Extract extracts the source directory from the SrcPkg into the specified directory.
func (srcpkg *SrcPkg) Extract(destdir string) (*SrcDir, os.Error) {
	dirname, err := srcpkg.PackageName()
	if err != nil {
		return nil, err
	}

	oldmask := syscall.Umask(0033)
	defer syscall.Umask(oldmask)

	destpkgdir := path.Join(destdir, dirname)
	rdr := srcpkg.reader
	for {
		hdr, err := rdr.Next()
		if err == os.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		
		switch hdr.Typeflag {
		case tar.TypeDir:
			if tardir := strings.TrimRight(hdr.Name, "/"); tardir != dirname {
				return nil, os.NewError("Tarball dir (" + hdr.Name + ") should be " + dirname)
			}
			if err := prepDirectory(destpkgdir); err != nil {
				return nil, err
			}
		case tar.TypeSymlink, tar.TypeLink:
			return nil, os.NewError("Links were found inside the source package, aborting.")
		case tar.TypeReg, tar.TypeRegA:
			dir, filename := path.Split(hdr.Name)
			dir = strings.TrimRight(dir, "/")
			if dir != dirname {
				errstr := fmt.Sprintf("File (%s) in source package is not contained in the " +
					"package dir (%s)", hdr.Name, dirname)
				return nil, os.NewError(errstr)
			}

			srcpkg.extractFile(path.Join(destpkgdir, filename), hdr)
		default:
			return nil, os.NewError("Invalid tar header type: " + string(hdr.Typeflag))
		}
	}
	
	return OpenSrcDir(destpkgdir)
}

// prepDirectory creates a new directory unless one already exists.
func prepDirectory(newpath string) (os.Error) {
	switch stat, err := os.Stat(newpath); {
	case err == nil:
		// If directory already exists that's cool, too.
		if stat.IsDirectory() { return nil }
		return os.NewError(newpath + " already exists as non-directory")
	case err.(*os.PathError).Error.String() == "no such file or directory":
		// Nothing is in the way.
	default:
		return err
	}
	return os.Mkdir(newpath, 0755)
}

func (srcpkg *SrcPkg) extractFile(newpath string, hdr *tar.Header) os.Error {
	file, err := os.Create(newpath)
	if err != nil {
		return err
	}

	_, err = io.Copy(file, srcpkg.reader)
	file.Close()
	if err != nil {
		return err
	}

	ubuf := &syscall.Utimbuf{int32(hdr.Atime), int32(hdr.Mtime)}
	if errno := syscall.Utime(newpath, ubuf); errno != 0 {
		return os.NewError("Failed to set modification time for "+newpath)
	}

	return nil
}

// Make extracts the srcpkg to the buildroot, then builds the binary package using
// makepkg.
// PKGDEST should be set before calling this func to force where the binary package will end up.
// Returns the path to the package files and nil on success; nil and error on failure.
func (srcpkg *SrcPkg) Make(buildroot string) ([]string, os.Error) {
	buildpath, err := filepath.Abs(buildroot)
	if err != nil {
		return nil, err
	}
	srcdir, err := srcpkg.Extract(buildpath)
	if err != nil {
		return nil, err
	}
	builtpkgs, err := srcdir.makepkg()
	if err != nil {
		return nil, err
	}
	return builtpkgs, nil
}

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

func openBuildLog(builddir string) (*os.File, os.Error) {
	tm := time.LocalTime()
	suffidx, suffix := 1, ""
	for {
		fname := fmt.Sprintf("mawbuild-%02d%02d%s.log", tm.Month, tm.Day, suffix)
		fqp := path.Join(builddir, fname)
		switch f, err := os.OpenFile(fqp, os.O_CREATE | os.O_WRONLY | os.O_EXCL, 0644); {
		case err == nil: return f, nil
		case err.(*os.PathError).Error.String() != "file exists": return nil, err
		}
		
		suffidx++
		suffix = fmt.Sprintf("-%d", suffidx)
	}
	return nil, os.NewError("Internal error: openBuildLog failed")
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
	// Open our logfile before we Chdir.
	outlog, err := openBuildLog(srcdir.builddir)
	if err != nil {
		return nil, err
	}
	defer outlog.Close()

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
	files := []*os.File{nil, outlog, outlog}
	attr := &os.ProcAttr{srcdir.builddir, nil, files}

	// Start it up and wait for it to finish.
	proc, err := os.StartProcess("/bin/bash", args, attr)
	if err != nil {
		return nil, err
	}
	defer proc.Release()
	status, err := proc.Wait(0)
	if err != nil {
		return nil, err
	}
	if code := status.ExitStatus(); code != 0 {
		return nil, os.NewError("makepkg failed")
	}

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

/*	srcpkg.go
	Contains the SrcPkg class and PackageBuilder class. SrcPkgs represent, hey
	guess what, source package files. The main purpose of the class is to
	extract the source package into a source directory. The PackageBuilder
	takes this source directory and, you guessed it, builds a binary package.
*/

package main

import (
	"io"
	"os"
	"fmt"
	"time"
	"path"
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

// PackageName extracts the name of the package from the path of the source package
// tarball.
func (srcpkg *SrcPkg) PackageName() (string, os.Error) {
	filename := path.Base(srcpkg.path)
	endidx := strings.Index(filename, ".")
	if endidx == -1 {
		return "", os.NewError("Invalid source package filename: " + filename)
	}
	pkgname := filename[0:endidx]
	return pkgname, nil
}

// Extract extracts the source directory from the SrcPkg into the specified
// destination directory.
//
// str1ng's goarchive (https://github.com/str1ngs/goarchive) was used as a
// starting point for this code and associated functions.
//
// TODO: do package checking first, maybe with a Check method.
func (srcpkg *SrcPkg) Extract(destdir string) (string, os.Error) {
	dirname, err := srcpkg.PackageName()
	if err != nil {
		return "", err
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
			return "", err
		}
		
		switch hdr.Typeflag {
		case tar.TypeDir:
			if tardir := strings.TrimRight(hdr.Name, "/"); tardir != dirname {
				msg := "Tarball dir ("+hdr.Name+") should be "+dirname
				return "", os.NewError(msg)
			}
			if err := prepDirectory(destpkgdir); err != nil {
				return "", err
			}
		case tar.TypeSymlink, tar.TypeLink:
			msg := "Links were found inside the source package, aborting."
			return "", os.NewError(msg)
		case tar.TypeReg, tar.TypeRegA:
			dir, filename := path.Split(hdr.Name)
			dir = strings.TrimRight(dir, "/")
			if dir != dirname {
				tmp := "File (%s) in source package is not contained "+
					"in the package dir (%s)"
				errstr := fmt.Sprintf(tmp, hdr.Name, dirname)
				return "", os.NewError(errstr)
			}
			srcpkg.extractFile(path.Join(destpkgdir, filename), hdr)
		default:
			return "", os.NewError("Invalid tar header type")
		}
	}
	
	return destpkgdir, nil
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

// extractFile is a helper function for Extract which extracts a file and
// matches the new file's mtime and atime to the original archive entry.
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

//////////////////////////////////////////////////////////////////////////////

type PackageBuilder struct {
	spawner SlaveSpawner
}

func NewPackageBuilder(spawner SlaveSpawner) *PackageBuilder {
	return &PackageBuilder{spawner}
}

func isDirectory(dirpath string) bool {
	pathinfo, err := os.Stat(dirpath)
	if err != nil || ! pathinfo.IsDirectory() {
		return false
	}
	return true
}

// Build runs makepkg on the specified srcdir. A slice of package paths are
// returned. These are the paths to the binary packages that are built. Packages
// with multiple pkgnames build multiple packages, hence the use of a slice.
// If an error occurs, returns nil and the error.
//
// Notice that we do not actually set PKGDEST ourselves, this should be done
// before calling this function. Otherwise the built package will just end up
// in the package source directory. Maybe.
func (builder *PackageBuilder) Build(srcdir string) ([]string, os.Error) {
	// Open our logfile before we Chdir.
	outlog, err := openBuildLog(srcdir)
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

	// Arguments after "-c" "..." override positional arguments $0, $1, ...
	cmd := []string{"/bin/bash", "-c", bashcode, "makepkg", "-m", "-f"}

	// We use StartSlaveProcess from main.go to embed a pipe the connects to our
	// master process. PKGDEST and MAWSECRET env variables should already be set
	proc, err := builder.spawner.SpawnSlaveProcess(cmd, srcdir, outlog)
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

	return readFileLines(tmpfile)
}

func openBuildLog(builddir string) (*os.File, os.Error) {
	tm := time.LocalTime()
	suffidx, suffix := 1, ""
	for {
		fname := fmt.Sprintf("mawbuild-%02d%02d%s.log",
			tm.Month, tm.Day, suffix)
		fqp := path.Join(builddir, fname)
		f, err := os.OpenFile(fqp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644);
		switch {
		case err == nil:
			return f, nil
		case err.(*os.PathError).Error.String() != "file exists":
			return nil, err
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

func readFileLines(f *os.File) ([]string, os.Error) {
	// Read our sneaky tempfile. It contains the names of package files
	// that were built by makepkg.
	pkgpaths := make([]string, 0, 32)

	// Use bufio to read one line/path at a time.
	reader := bufio.NewReader(f)
ResultLoop:
	for {
		line, prefix, err := reader.ReadLine()
		switch {
		default:
			pkgpaths = append(pkgpaths, string(line))
		case prefix:
			return nil, os.NewError("Extremely long line for package path")
		case err == os.EOF:
			break ResultLoop
		case err != nil:
			return nil, err
		}
	}

	return pkgpaths, nil
}

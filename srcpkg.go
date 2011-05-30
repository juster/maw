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
	"path"
	"exec"
	"bufio"
	"strings"
	"syscall"
	"io/ioutil"
	"archive/tar"
	"compress/gzip"
)

const (
	MawMakepkgPath = "/usr/bin/mawmakepkg"
)

type SrcPkg struct {
	path     string
	file     *os.File
	unzipper *gzip.Decompressor
	reader   *tar.Reader
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
				msg := "Tarball dir (" + hdr.Name + ") should be " + dirname
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
				tmp := "File (%s) in source package is not contained " +
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
func prepDirectory(newpath string) os.Error {
	switch stat, err := os.Stat(newpath); {
	case err == nil:
		// If directory already exists that's cool, too.
		if stat.IsDirectory() {
			return nil
		}
		return os.NewError(newpath + " already exists as non-directory")
	case err.(*os.PathError).Error.String() == "no such file or directory":
		// Nothing is in the way.
	default:
		return err
	}
	oldmask := syscall.Umask(0)
	defer syscall.Umask(oldmask)
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
		return os.NewError("Failed to set modification time for " + newpath)
	}

	return nil
}

//////////////////////////////////////////////////////////////////////////////

type PackageBuilder struct {
	customLevel int //unimplemented
}

func NewPackageBuilder() *PackageBuilder {
	return &PackageBuilder{}
}

func isDirectory(dirpath string) bool {
	pathinfo, err := os.Stat(dirpath)
	if err != nil || !pathinfo.IsDirectory() {
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
	// Create a tempfile and hook it into our bash tomfoolery.
	pathfile, err := NewPkgPathFile()
	if err != nil {
		return nil, err
	}
	defer pathfile.Cleanup()

	// Call our included utility, mawmakepkg which wraps makepkg to drop priveledges
	// and print the paths of built packages to our tempfile.
	args := []string{MawMakepkgPath, pathfile.Name(), "-s", "-m", "-f"}
	cmd, err := exec.Run(MawMakepkgPath, args, nil, srcdir,
		exec.PassThrough, exec.PassThrough, exec.PassThrough)
	if err != nil {
		return nil, err
	}
	defer cmd.Process.Release()

	waitmsg, err := cmd.Wait(0)
	if code := waitmsg.ExitStatus(); code != 0 {
		return nil, os.NewError("makepkg failed")
	}

	return pathfile.ReadLines()
}

type PkgPathFile struct {
	file *os.File
	path string
}

func NewPkgPathFile() (*PkgPathFile, os.Error) {
	tmpfile, err := ioutil.TempFile("", "maw")
	if err != nil {
		return nil, err
	}
	pathfile := &PkgPathFile{tmpfile, tmpfile.Name()}

	// If we are running under sudo, we must chmod the tempfile to the user
	// whom we are going to be dropping privledges to (the SUDO_USER).
	sudouser := lookupSudoUser()
	if sudouser == nil {
		return pathfile, nil
	}
	tmpfile.Chown(sudouser.Uid, sudouser.Gid)
	return pathfile, nil
}

func (ppfile *PkgPathFile) Name() string {
	return ppfile.file.Name()
}

func (ppfile *PkgPathFile) Cleanup() {
	ppfile.file.Close()
	os.Remove(ppfile.path)
}

func (ppfile *PkgPathFile) ReadLines() ([]string, os.Error) {
	// Read our sneaky tempfile. It contains the names of package files
	// that were built by makepkg.
	pkgpaths := make([]string, 0, 32)

	// Use bufio to read one line/path at a time.
	reader := bufio.NewReader(ppfile.file)
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

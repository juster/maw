package main

import (
	"io"
	"os"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"syscall"
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

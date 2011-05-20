package maw

import (
    "os"
    "rand"
    "time"
    "bufio"
    "strings"
)

type SrcPkg struct {
    path string
}

type SrcDir struct {
    builddir string
}

// Constructors simply double-check that the source package or
// build directory do in fact exist.

func NewSrcPkg(path string) (*SrcPkg, os.Error) {
    if _, err := os.Open(path); err != nil {
        return nil, os.NewError("Failed to read source package at " + path)
    }
    return &SrcPkg{ path }, nil
}

func NewSrcDir(path string) (*SrcDir, os.Error) {
    if _, err := os.Open(path); err != nil {
        return nil, os.NewError("Failed to read source directory at " + path)
    }
    return &SrcDir{ path }, nil
}

// Makepkg extracts the tarball to the buildroot, then builds the binary package using
// makepkg. PKGDEST should be set before calling this func to force where
// the binary package will end up.
// Returns the path to the package file and nil on success; nil and error on failure.
func (srcpkg *SrcPkg) Make(buildroot string) ([]string, os.Error) {
    srcdir, err := srcpkg.Untar(buildroot)
    if err != nil { return nil, err }
    builtpkgs, err := srcdir.makepkg()
    if err != nil { return nil, err }
    return builtpkgs, nil
}

// srcFilePkgName extracts the name of the package from the path of the
// source package tarball.
func srcFilePkgName (pkgpath string) (string, os.Error) {
    // Guess the name of the directory that was extracted under buildroot
    begidx := strings.LastIndex(pkgpath, "/")
    if begidx == -1 { begidx = 0 } else { begidx++ }
    filename := pkgpath[begidx:]
    endidx := strings.Index(filename, ".")
    if endidx == -1 {
        return "", os.NewError("Invalid source package filename: " + filename)
    }
    pkgname := filename[0:endidx]
    return pkgname, nil
}

// Untar uses the tar program to extract the source tarball to our buildroot (destdir).
// Returns a SrcDir pointer or nil and an error on failure.
func (srcpkg *SrcPkg) Untar (destdir string) (*SrcDir, os.Error) {
    // If TAR_OPTIONS is set, it will be used by tar and affect its behavior.
    if oldval := os.Getenv("TAR_OPTIONS"); oldval != "" {
        os.Setenv("TAR_OPTIONS", "")
        defer os.Setenv("TAR_OPTIONS", oldval)
    }
    
    args := []string{
        "tar",
        "--extract",
        "--file=" + srcpkg.path,
    }
    // TODO: use --no-... flags just in case we are root? (we shouldn't be...)
    
    files := []*os.File{ os.Stdin, os.Stdout, os.Stderr }
    procattr := &os.ProcAttr{ destdir, nil, files }
    tarproc, err := os.StartProcess("/bin/tar", args, procattr)
    if err != nil { return nil, err }
    
    var status *os.Waitmsg
    switch status, err = tarproc.Wait(0); {
    case err != nil: return nil, err
    case status.WaitStatus != 0: return nil, os.NewError(status.String())
    }
    tarproc.Release()
    
    pkgname, err := srcFilePkgName(srcpkg.path)
    if (err != nil) { return nil, err }
    return NewSrcDir(destdir + "/" + pkgname)
}

// Creates a random filename under /tmp and makes sure it is
// not already taken.
func tmpFileName() string {
    var filename string
    chars := [8]int{}
    for {
        src := rand.NewSource(time.Seconds())
        r := rand.New(src)
        for i := 0 ; i < 8 ; i++ {
            chars[i] = 'a' + r.Intn('z' - 'a')
        }
        filename = os.TempDir() + "/" + string(chars[:])
        if _, err := os.Stat(filename); err != nil {
            // Stat will fail if file does not exist, which is what we want.
            break
        }
    }
    return filename
}

// Muahahaha!
// This creates the bash code we use to hook into makepkg. Makepkg
// will then print out the paths of the packages that it just created to
// the temporary filename we choose.
// Returns the bash code and temporary file name.
func bashHack() (string, string) {
    tmpfile := tmpFileName()
    bash := `
exit () {
  if [ "$1" -ne 0 ] ; then command exit $1 ; fi
  fullver=$(get_full_version $epoch $pkgver $pkgrel)
  for pkg in ${pkgname[@]} ; do
    for arch in "$CARCH" any ; do
      pkgfile="${PKGDEST}/${pkg}-${fullver}-${arch}${PKGEXT}"
      if [ -f "$pkgfile" ] ; then
        echo "$pkgfile" >>` + tmpfile + `
      fi
    done
  done
  command exit 0
}
source makepkg
`
    return bash, tmpfile
}

// makepkg runs makepkg on the specified builddir. The resulting package is
// searched for in destdir. Notice we do not actually set PKGDEST ourselves, this
// should be done before calling this function.
// Returns the paths of built packages or nil and error if makepkg fails.
func (srcdir *SrcDir) makepkg () ([]string, os.Error) {
    // Chdir to builddir. Chdir back on func exit.
    olddir,err := os.Getwd()
    if err != nil { return nil, err }
    defer os.Chdir(olddir)
    err = os.Chdir(srcdir.builddir)
    if err != nil { return nil, err }
    
    bashcode, tmpfilename := bashHack()
    
    // We must force $0 to be makepkg... makepkg runs $0 internally.
    // Arguments after "-c" "..." override positional arguments $0, $1, ...
    args := []string{ "bash", "-c", bashcode, "makepkg", "-m", "-f"}

    // Prepare to rock makepkg's world!
    outnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0666)
    if err != nil { return nil, err }
    errnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0666)
    if err != nil { return nil, err }
    files := []*os.File{ nil, os.Stdout, os.Stderr }
    attr := &os.ProcAttr{ "", nil, files }
    
    // Start it up and wait for it to finish.
    proc, err := os.StartProcess("/bin/bash", args, attr)
    if err != nil { return nil, err }
    status, err := proc.Wait(0)
    if err != nil { return nil, err }
    if code := status.ExitStatus(); code != 0 {
        return nil, os.NewError("makepkg failed")
    }
    outnull.Close()
    errnull.Close()
    proc.Release()

    // Read our sneaky tempfile. It contains the names of package files
    // that were built by makepkg.
    pkgpaths := make([]string, 0, 32)
    tmpfile, err := os.Open(tmpfilename)
    if err != nil { return nil, err }

    // Use bufio to read one line/path at a time.
    reader := bufio.NewReader(tmpfile)
    RESULTLOOP:
    for {
        line, prefix, err := reader.ReadLine()
        switch {
        default: pkgpaths = append(pkgpaths, string(line))
        case prefix: return nil, os.NewError("Extremely long line for package path")
        case err == os.EOF: break RESULTLOOP
        case err != nil: return nil, err
        }
    }
    tmpfile.Close()
    err = os.Remove(tmpfilename)
    if err != nil { return nil, err }
    
    return pkgpaths, nil
}

/* aur.go
 * Code for interfacing with the AUR.
 * Justin Davis <jrcd83 at googlemail>
 */

package main

import (
	"io"
	"os"
	"http"
	"fmt"
	"path"
	"time"
)

const (
	AUR_ROOT = "http://aur.archlinux.org"
)

type AURCache struct {
	srcpkgdest, buildroot string
	builder               *PackageBuilder
}

func NewAURCache(srcdest, buildroot string, builder *PackageBuilder) *AURCache {
	return &AURCache{srcdest, buildroot, builder}
}

func (aur *AURCache) srcPkgPath(pkgname string) string {
	return fmt.Sprintf("%s/%s.src.tar.gz", aur.srcpkgdest, pkgname)
}

func (aur *AURCache) Fetch(pkgname string) ([]string, FetchError) {
	srcpath, err := aur.downloadNewer(pkgname)
	if err != nil {
		return nil, FetchErrorWrap(pkgname, err)
	}
	if srcpath == "" {
		return nil, NotFoundError(pkgname)
	}

	srcpkg, err := OpenSrcPkg(srcpath)
	if err != nil {
		return nil, FetchErrorWrap(pkgname, err)
	}

	// If we are running under sudo, we do not want our files to be owned by root.
	uid, gid := lookupSudoUser()
	if uid != 0 {
		os.Chown(srcpath, uid, gid)
	}

	srcdir, err := srcpkg.Extract(aur.buildroot)
	srcpkg.Close()
	if err != nil {
		return nil, FetchErrorWrap(pkgname, err)
	}

	if uid != 0 {
		chownDirRec(srcdir, uid, gid)
	}

	pkgpaths, err := aur.builder.Build(srcdir)
	if err != nil {
		return nil, FetchErrorWrap(pkgname, err)
	}

	return pkgpaths, nil
}

func chownDirRec(dir string, uid, gid int) {
	dirh, err := os.Open(dir)
	if err != nil {
		return
	}

	dirh.Chown(uid, gid)
	names, err := dirh.Readdirnames(-1)
	if err != nil {
		return
	}
	for _, entry := range names {
		os.Chown(path.Join(dir, entry), uid, gid)
	}
}

// mtimeDateStr converts the file modification time into a date string that HTTP likes.
func mtimeDateStr(mtime int64) string {
	// mtime is in nanoseconds! (one _billionth_ of a second)
	t := time.SecondsToUTC(mtime / 1000000000)
	t.Zone = "GMT"
	return t.Format(time.RFC1123)
}

func srcPkgUrl(pkgname string) string {
	return fmt.Sprintf("%s/packages/%s/%s.tar.gz", AUR_ROOT, pkgname, pkgname)
}

func (aur *AURCache) downloadNewer(pkgname string) (string, os.Error) {
	var mtime int64
	path := aur.srcPkgPath(pkgname)
	if stat, _ := os.Stat(path); stat != nil {
		mtime = stat.Mtime_ns
	}
	url := srcPkgUrl(pkgname)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	if mtime != 0 {
		date := mtimeDateStr(mtime)
		req.Header.Add("If-Modified-Since", date)
	}
	req.UserAgent = MAW_USERAGENT

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
		break
	case 304:
		if mtime == 0 {
			return "", os.NewError("Received HTTP not modified without requesting it")
		}
		return path, nil
	default:
		return "", err
	}
	var destfile *os.File
	if destfile, err = os.Create(path); err != nil {
		return "", err
	}
	defer destfile.Close()
	if _, err = io.Copy(destfile, resp.Body); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

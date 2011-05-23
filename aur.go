/* aur.go
 * Code for interfacing with the AUR.
 * Justin Davis <jrcd83 at googlemail>
 */

package main

import (
	"os"
	"http"
	"fmt"
	"io"
	"time"
)

const (
	AUR_ROOT = "http://aur.archlinux.org"
	MAW_USERAGENT = "maw/1.0"
)

type FetchError struct {
	NotFound bool
	Query    string
	Message  string
}

func (err *FetchError) String() string {
	if err.NotFound {
		return fmt.Sprintf("Package '%s' not found", err.Query)
	}
	return err.Message
}

func NewFetchError(pkgname string, err os.Error) *FetchError {
	return &FetchError{false, pkgname, err.String()}
}

func NotFoundError(pkgname string) *FetchError {
	return &FetchError{true, pkgname, ""}
}

type PackageFetcher interface {
	Fetch(pkgname string) ([]string, *FetchError)
}

type AURCache struct {
	Pkgdest    string
	Srcpkgdest string
	Buildroot  string
}

func (aur *AURCache) srcPkgPath(pkgname string) string {
	return fmt.Sprintf("%s/%s.src.tar.gz", aur.Srcpkgdest, pkgname)
}

func (aur *AURCache) Fetch(pkgname string) ([]string, *FetchError) {
	path, err := aur.downloadNewer(pkgname)
	if err != nil {
		return nil, NewFetchError(pkgname, err)
	}
	if path == "" {
		return nil, NotFoundError(pkgname)
	}

	srcpkg, err := OpenSrcPkg(path)
	if err != nil {
		return nil, NewFetchError(pkgname, err)
	}
	pkgpaths, err := srcpkg.Make(aur.Buildroot)
	srcpkg.Close()
	if err != nil {
		return nil, NewFetchError(pkgname, err)
	}

	return pkgpaths, nil
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
	fmt.Printf("*DBG* StatusCode=%d\n", resp.StatusCode)
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

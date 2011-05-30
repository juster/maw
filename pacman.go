/*	pacman.go
	Pacman package file fetching.
*/

package main

import (
	"os"
	"io"
	"bufio"
	"exec"
	"path"
	"http"
	"strings"
)

type PacmanFetcher struct {
	pkgdest string
}

func (pf *PacmanFetcher) readLine(readhandle io.Reader) string {
	buf := bufio.NewReader(readhandle)
	line, _, _ := buf.ReadLine()
	return string(line)
}

func (pf *PacmanFetcher) findPackageUrl(pkgname string) (string, FetchError) {
	args := []string{"pacman", "-S", "--print", pkgname}
	cmd, err := exec.Run("/usr/bin/pacman", args, nil, "",
		exec.DevNull, exec.Pipe, exec.Pipe)
	if err != nil {
		return "", FetchErrorWrap(pkgname, err)
	}
	defer cmd.Close()

	waitmsg, err := cmd.Wait(0)
	if err != nil {
		return "", FetchErrorWrap(pkgname, err)
	}

	if code := waitmsg.ExitStatus(); code != 0 {
		errline := pf.readLine(cmd.Stderr)
		if errline == "error: target not found: "+pkgname {
			return "", NotFoundError(pkgname)
		}
		return "", NewFetchError(pkgname, "pacman "+errline)
	}

	url := pf.readLine(cmd.Stdout)
	return url, nil
}

func (pf *PacmanFetcher) Fetch(pkgname string) ([]string, FetchError) {
	urltext, err := pf.findPackageUrl(pkgname)
	if err != nil {
		return nil, err
	}

	url, oserr := http.ParseURL(urltext)
	if oserr != nil {
		return nil, FetchErrorWrap(pkgname, oserr)
	}

	var pkgpath string

	switch url.Scheme {
	case "http":
		fallthrough
	case "https":
		pkgpath, oserr = pf.httpDownload(url)
	case "ftp":
		pkgpath, oserr = pf.ftpDownload(url)
	default:
		return nil, NewFetchError(pkgname, "Unrecognized URL scheme: "+url.Scheme)
	}

	if oserr != nil {
		return nil, FetchErrorWrap(pkgname, oserr)
	}

	return []string{pkgpath}, nil
}

func (pf *PacmanFetcher) ftpDownload(url *http.URL) (string, os.Error) {
	host, rpath := url.Host, url.Path
	if strings.Index(host, ":") == -1 {
		host = host + ":21"
	}

	_, filename := path.Split(rpath)
	destpath := path.Join(pf.pkgdest, filename)

	ftp, err := DialFtp(host)
	if err != nil {
		return "", err
	}
	rdr, err := ftp.Fetch(rpath)
	if err != nil {
		return "", err
	}
	defer ftp.Close()

	destfile, err := os.Create(destpath)
	if err != nil {
		return "", err
	}

	_, err = io.Copy(destfile, rdr)
	if err != nil {
		return "", err
	}
	destfile.Close()

	return destpath, nil
}

func (pf *PacmanFetcher) httpDownload(url *http.URL) (string, os.Error) {
	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return "", err
	}

	req.UserAgent = MAW_USERAGENT
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", os.NewError("Download of " + url.String() + " failed: HTTP " + resp.Status)
	}

	_, filename := path.Split(url.Path)
	destpath := path.Join(pf.pkgdest, filename)
	destfile, err := os.Create(destpath)
	if err != nil {
		return "", err
	}
	defer destfile.Close()

	if resp.ContentLength < 0 {
		_, err = io.Copy(destfile, resp.Body)
	} else {
		_, err = io.Copyn(destfile, resp.Body, resp.ContentLength)
	}
	if err != nil {
		return "", err
	}

	return destpath, nil
}

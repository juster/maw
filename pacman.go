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
)

type PacmanFetcher struct {
	pkgdest string
}

func (pf *PacmanFetcher) readLine(readhandle io.Reader) (string) {
	buf := bufio.NewReader(readhandle)
	line, _, _ := buf.ReadLine()
	return string(line)
}

func (pf *PacmanFetcher) findPackageUrl(pkgname string) (string, *FetchError) {
	cmd, err := exec.Run("/usr/bin/pacman", []string{"pacman", "-S", "--print", pkgname}, nil, "",
		exec.DevNull, exec.Pipe, exec.Pipe)
	if err != nil {
		return "", NewFetchError(pkgname, err.String())
	}
	defer cmd.Close()
	
	waitmsg, err := cmd.Wait(0)
	if err != nil {
		return "", NewFetchError(pkgname, err.String())
	}
	
	if code := waitmsg.ExitStatus(); code != 0 {
		errline := pf.readLine(cmd.Stderr)
		if errline == "error: target not found: " + pkgname {
			return "", NotFoundError(pkgname)
		}
		return "", NewFetchError(pkgname, "pacman " + errline)
	}
	
	// TODO: make sure it is a url
	url := pf.readLine(cmd.Stdout)
	return url, nil
}

func (pf *PacmanFetcher) Fetch(pkgname string) ([]string, *FetchError) {
	urltext, err := pf.findPackageUrl(pkgname)
	if err != nil {
		return nil, err
	}
	
	pkgpath, oserr := pf.downloadPackage(urltext)
	if oserr != nil {
		return nil, NewFetchError(pkgname, oserr.String())
	}
	
	return []string{pkgpath}, nil
}

func (pf *PacmanFetcher) downloadPackage(urltext string) (string, os.Error) {
	url, err := http.ParseURL(urltext)
	if err != nil {
		return "", err
	}
	
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
		return "", os.NewError("Download of "+urltext+" failed: HTTP "+resp.Status)
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

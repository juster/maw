/*	fetch.go
	Package fetching interface and package fetching error interface.
*/

package main

import (
	"fmt"
	"os"
)

type FetchError interface {
	// returns an error message, like os.Error
	String() string
	// returns true if no matching package name could be found
	NotFound() bool
}

type FetchErrorRaw struct {
	missing bool
	query   string
	message string
}

type FetchErrorWrapper struct {
	pkgname string
	oserr   os.Error
}

func (err *FetchErrorRaw) String() string {
	if err.missing {
		return fmt.Sprintf("error: target not found %s", err.query)
	}
	return err.message
}

func (err *FetchErrorRaw) NotFound() bool {
	return err.missing
}

func NewFetchError(pkgname string, errmsg string) FetchError {
	return &FetchErrorRaw{false, pkgname, errmsg}
}

// NotFoundError creates a FetchError for pkgname which returns true for NotFound()
func NotFoundError(pkgname string) FetchError {
	return &FetchErrorRaw{true, pkgname, ""}
}

// FetchErrorWrap wraps an os.Error into a FetchError
func FetchErrorWrap(pkgname string, oserr os.Error) FetchError {
	return &FetchErrorWrapper{pkgname, oserr}
}

func (wrap *FetchErrorWrapper) String() string {
	return wrap.oserr.String()
}

func (wrap *FetchErrorWrapper) NotFound() bool {
	return false
}

type PackageFetcher interface {
	Fetch(pkgname string) ([]string, FetchError)
}

////////////////////////////////////////////////////////////////////////////////

// The MultiFetcher not only fetches from multiple other fetchers but also fetches multiple
// packages at the same time! Zing!
type MultiFetcher struct {
	fetchers []PackageFetcher
}

type fetchResult struct {
	pkgs  []string
	error FetchError
}

func NewMultiFetcher(fetchers ...PackageFetcher) *MultiFetcher {
	return &MultiFetcher{fetchers}
}

func (mf *MultiFetcher) FetchAll(pkgnames []string) ([]string, os.Error) {
	// Packages are all fetched concurrently, independent of each other
	chans := make([]chan *fetchResult, len(pkgnames))
	for i, pkgname := range pkgnames {
		r := make(chan *fetchResult, 1)
		go mf.chanFetch(pkgname, r)
		chans[i] = r
	}

	// Waits for all goroutines to finish, collecting results
	allpkgpaths := make([]string, 0, 256) // TODO: use cap or something?
	for i, c := range chans {
		result := <-c
		if result.error == nil {
			allpkgpaths = append(allpkgpaths, result.pkgs...)
		} else if result.error.NotFound() {
			return nil, os.NewError("could not find " + pkgnames[i])
		} else {
			return nil, result.error
		}
	}

	return allpkgpaths, nil
}

// chanFetch is a simple wrapper to make Fetch more concurrent.
func (mf *MultiFetcher) chanFetch(pkgname string, results chan *fetchResult) {
	paths, err := mf.Fetch(pkgname)
	results <- &fetchResult{paths, err}
}

func (mf *MultiFetcher) Fetch(pkgname string) ([]string, FetchError) {
	var pkgpaths []string

SearchLoop:
	for _, fetcher := range mf.fetchers {
		var err FetchError
		pkgpaths, err = fetcher.Fetch(pkgname)
		if pkgpaths != nil {
			return pkgpaths, nil
		} else {
			if err.NotFound() {
				continue SearchLoop
			}
			return nil, err
		}
	}

	return nil, NotFoundError(pkgname)
}

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

/*	fetch.go
	Package fetching.
*/

package main

import (
	"fmt"
)

const (
	MAW_USERAGENT = "maw/1.0"
)

type FetchError struct {
	NotFound bool
	Query    string
	Message  string
}

func (err *FetchError) String() string {
	if err.NotFound {
		return fmt.Sprintf("error: target not found %s", err.Query)
	}
	return err.Message
}

func NewFetchError(pkgname string, errmsg string) *FetchError {
	return &FetchError{false, pkgname, errmsg}
}

func NotFoundError(pkgname string) *FetchError {
	return &FetchError{true, pkgname, ""}
}

type PackageFetcher interface {
	Fetch(pkgname string) ([]string, *FetchError)
}

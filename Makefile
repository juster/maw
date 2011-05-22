include $(GOROOT)/src/Make.inc

TARG=maw
GOFILES=main.go maw.go srcpkg.go srcdir.go
CLEANFILES+=*.gz ./tmp/*

include $(GOROOT)/src/Make.cmd

install-goarchive:
	goinstall -u github.com/str1ngs/goarchive

format:
	gofmt -w -l *.go

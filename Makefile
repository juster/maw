include $(GOROOT)/src/Make.inc

TARG=maw
GOFILES=maw.go srcpkg.go
CLEANFILES+=*.gz

include $(GOROOT)/src/Make.pkg

install-goarchive:
	goinstall -u github.com/str1ngs/goarchive

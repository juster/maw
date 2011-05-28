include $(GOROOT)/src/Make.inc

TARG=maw
GOFILES=main.go fetch.go ftp.go pacman.go aur.go srcpkg.go
CLEANFILES+=*.gz ./tmp/*

include $(GOROOT)/src/Make.cmd

install-goarchive:
	goinstall -u github.com/str1ngs/goarchive

format:
	gofmt -w -l *.go

mawmakepkg: mawmakepkg.c
	gcc -o mawmakepkg mawmakepkg.c

maw: mawmakepkg

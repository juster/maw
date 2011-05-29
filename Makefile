ifeq ($(shell uname -m), x86_64)
  GOC:=6g
  GOL:=6l
else
  GOC:=8g
  GOL:=8l
endif

.PHONY: test all

all: maw mawmakepkg

maw: main.8
	$(GOL) -o maw $^

main.8: main.go aur.go fetch.go ftp.go main.go pacman.go srcpkg.go

%.8: %.go
	$(GOC) $^

mawmakepkg: mawmakepkg.c
	gcc -o mawmakepkg mawmakepkg.c

install: all
	install -d -m 755 $(DESTDIR)/usr/bin
	install -m 755 -t $(DESTDIR)/usr/bin maw mawmakepkg

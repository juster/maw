.PHONY: test all

all: maw

maw.8: maw.go srcpkg.go

maw: main.8 maw.8
	8l -o maw main.8

test: maketest
	-rm *.pkg.tar*
	./maketest

%.8: %.go
	8g $^

maketest: maketest.8
	8l -o maketest maketest.8

package main

import (
	"testing"
)

func TestTest(t *testing.T) {
	ac := &AURCache{"./tmp", "./tmp", "./tmp"}
	_, err := ac.Fetch("cower-git")
	if err != nil {
		t.Error(err)
	}
}

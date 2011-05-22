/* maw.go
 * Makepkg Aur Wrapper - Main program code
 * Justin Davis <jrcd83 at googlemail>
 */

package main

import (
	"os"
	"fmt"
)

func main() {
	aur := &AURCache{".", ".", "."}
	for _, arg := range os.Args[1:] {
		paths, err := aur.Fetch(arg)
		if err != nil {
			fmt.Printf("ERROR: %s\n", err.String())
		} else {
			fmt.Printf("*DBG* paths=%v\n", paths)
		}
	}
}

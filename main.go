/* maw.go
 * Makepkg Aur Wrapper - Main program code
 * Justin Davis <jrcd83 at googlemail>
 */

package maw

import (
	"fmt"
)

func main() {
	aur := &AURCache{".", ".", "."}
	paths, err := aur.Fetch("perl-cower-git")
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.String())
	} else {
		fmt.Printf("*DBG* paths=%v\n", paths)
	}
}

/* maw.go
 * Makepkg Aur Wrapper - Main program code
 * Justin Davis <jrcd83 at googlemail>
 */

package main

import (
	"os"
	"fmt"
)

func fetchPackage(c chan []string, fetchers []PackageFetcher, pkgname string) {
	for _, fetcher := range fetchers {
		paths, err := fetcher.Fetch(pkgname)
		if err != nil {
			if err.NotFound {

				continue
			}
			fmt.Printf("ERROR: %s\n", err.String())
			c <- nil
			return
		}
		c <- paths
		return
	}

	fmt.Printf("ERROR: package %s was not found\n", pkgname)
	c <- nil
}

func main() {
	fetchers := make([]PackageFetcher, 2, 8)
	fetchers[0] = &AURCache{".", ".", "."}
	fetchers[1] = &PacmanFetcher{"."}

	pkgchan := make(chan []string, 8)
	for _, arg := range os.Args[1:] {
		go fetchPackage(pkgchan, fetchers, arg)
	}

	fmt.Printf("DBG: len=%d\n", len(os.Args)-1)
	i := 0
	for i < len(os.Args)-1 {
		for _, path := range <- pkgchan {
			fmt.Printf("Package: %s\n", path )
		}
		i++
		fmt.Printf("DBG: i=%d\n", i)
	}
}

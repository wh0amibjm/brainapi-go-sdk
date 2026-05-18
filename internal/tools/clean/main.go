// clean removes build artifacts. Cross-platform alternative to `rm -rf`
// in the Makefile, since Windows shells lack rm by default.
package main

import (
	"fmt"
	"os"
)

func main() {
	targets := []string{"bin", "coverage.out", "coverage.html"}
	for _, t := range targets {
		if err := os.RemoveAll(t); err != nil {
			fmt.Fprintf(os.Stderr, "clean: %s: %v\n", t, err)
			os.Exit(1)
		}
	}
}

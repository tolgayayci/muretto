// Command muretto shows what your AI coding agents created outside the repo.
// It is read-only: it never changes anything in a provider account.
package main

import (
	"fmt"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("muretto", version)
		return
	}
	fmt.Fprintln(os.Stderr, "muretto: not implemented yet")
	os.Exit(1)
}

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/AmirSyafiq2112/lazydbm/internal/tui"
)

// version is set by GoReleaser via ldflags.
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lazydbm: working directory: %v\n", err)
		os.Exit(1)
	}

	if err := tui.Run(cwd, version); err != nil {
		fmt.Fprintf(os.Stderr, "lazydbm: %v\n", err)
		os.Exit(1)
	}
}

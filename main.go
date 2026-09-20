package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/AmirSyafiq2112/lazydbm/internal/tui"
)

// version is set by GoReleaser via ldflags.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Getwd, tui.Run, os.Stdout, os.Stderr))
}

func run(
	args []string,
	getwd func() (string, error),
	startTUI func(cwd, version string) error,
	stdout, stderr io.Writer,
) int {
	fs := flag.NewFlagSet("lazydbm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, version)
		return 0
	}

	cwd, err := getwd()
	if err != nil {
		fmt.Fprintf(stderr, "lazydbm: working directory: %v\n", err)
		return 1
	}

	if err := startTUI(cwd, version); err != nil {
		fmt.Fprintf(stderr, "lazydbm: %v\n", err)
		return 1
	}
	return 0
}

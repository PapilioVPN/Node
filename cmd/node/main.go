// Command node runs the PapilioVPN Node command-line interface.
package main

import (
	"os"

	"github.com/papilio-platform/Node/internal/buildinfo"
	"github.com/papilio-platform/Node/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, buildinfo.Read()))
}

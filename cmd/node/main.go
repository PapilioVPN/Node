// Command node runs the PapilioVPN Node command-line interface.
package main

import (
	"context"
	"os"

	"github.com/papilio-platform/Node/internal/buildinfo"
	"github.com/papilio-platform/Node/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr, buildinfo.Read()))
}

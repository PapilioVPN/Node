// Package cli adapts Node command-line arguments and output streams.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/papilio-platform/Node/internal/buildinfo"
)

const usage = `PapilioVPN Node

Usage:
  node help
  node version [--json]

Commands:
  help     Show available commands.
  version  Print binary version and build metadata.

Engine management is not available in this development build.
`

// Run executes a CLI command and returns its process exit code: 0 for success,
// 1 for an output failure, and 2 for invalid usage. The caller owns the writers.
func Run(args []string, stdout, stderr io.Writer, info buildinfo.Info) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch args[0] {
	case "help", "-h", "--help":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "help does not accept arguments")
			return 2
		}
		if _, err := io.WriteString(stdout, usage); err != nil {
			fmt.Fprintf(stderr, "write help: %v\n", err)
			return 1
		}
	case "version":
		return runVersion(args[1:], stdout, stderr, info)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	return 0
}

// versionOutput is the CLI's JSON wire contract, independent of internal records.
type versionOutput struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	Modified  bool   `json:"modified"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

func runVersion(args []string, stdout, stderr io.Writer, info buildinfo.Info) int {
	flags := flag.NewFlagSet("node version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "Print structured build metadata.")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "version does not accept positional arguments")
		return 2
	}

	var err error
	if *asJSON {
		output := versionOutput{
			Version:   info.Version,
			Revision:  info.Revision,
			Modified:  info.Modified,
			GoVersion: info.GoVersion,
			OS:        info.OS,
			Arch:      info.Arch,
		}
		err = json.NewEncoder(stdout).Encode(output)
	} else {
		_, err = fmt.Fprintf(stdout, "PapilioVPN Node %s (%s %s/%s)\n",
			info.Version, info.GoVersion, info.OS, info.Arch)
	}
	if err != nil {
		fmt.Fprintf(stderr, "write version: %v\n", err)
		return 1
	}
	return 0
}

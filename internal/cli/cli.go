// Package cli adapts Node command-line arguments and output streams.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/charmbracelet/colorprofile"

	"github.com/papilio-platform/Node/internal/buildinfo"
)

// Run executes a CLI command and returns its process exit code: 0 for success,
// 1 for an execution/output failure, and 2 for invalid usage. The caller owns
// the streams. Only the explicit tui command takes over an interactive terminal.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, info buildinfo.Info) int {
	flags := flag.NewFlagSet("node", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {} // Root help belongs on stdout, including --help.
	color := flags.String("color", "auto", "Color mode: auto, always, never (before the command).")
	parseErr := flags.Parse(args)
	if parseErr != nil && !errors.Is(parseErr, flag.ErrHelp) {
		return 2
	}
	if *color != "auto" && *color != "always" && *color != "never" {
		fmt.Fprintln(stderr, "color must be auto, always or never")
		return 2
	}
	styles := newPalette(colorEnabled(*color, stdout))
	humanOutput := &colorprofile.Writer{Forward: stdout, Profile: outputProfile(*color, stdout)}
	humanError := &colorprofile.Writer{Forward: stderr, Profile: outputProfile(*color, stderr)}
	args = flags.Args()
	if errors.Is(parseErr, flag.ErrHelp) {
		args = []string{"help"}
	}
	if len(args) == 0 {
		fmt.Fprint(humanError, renderHelp(newPalette(colorEnabled(*color, stderr))))
		return 2
	}

	switch args[0] {
	case "help", "-h", "--help":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "help does not accept arguments")
			return 2
		}
		if _, err := io.WriteString(humanOutput, renderHelp(styles)); err != nil {
			fmt.Fprintf(stderr, "write help: %v\n", err)
			return 1
		}
	case "version":
		return runVersion(args[1:], stdout, stderr, info, *color)
	case "tui":
		return runTUI(ctx, args[1:], stdin, stdout, stderr, info, *color)
	default:
		fmt.Fprintf(humanError, "unknown command %q\n\n%s", args[0], renderHelp(newPalette(colorEnabled(*color, stderr))))
		return 2
	}
	return 0
}

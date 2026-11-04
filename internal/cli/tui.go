package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/papilio-platform/Node/internal/buildinfo"
	"github.com/papilio-platform/Node/internal/terminal/theme"
	"github.com/papilio-platform/Node/internal/tui"
)

func runTUI(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, info buildinfo.Info, color string) int {
	flags := flag.NewFlagSet("node tui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: node [--color auto|always|never] tui\n\nOpen the interactive console. Requires terminal input/output and TERM other than dumb.\nUse Tab or 1/2/3 to navigate, ? for shortcuts and q to quit.")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "tui does not accept positional arguments")
		return 2
	}
	if !isTerminal(stdin) || !isTerminal(stdout) || os.Getenv("TERM") == "dumb" {
		fmt.Fprintln(stderr, "tui requires terminal input/output and TERM other than dumb; use node help or node version --json")
		return 2
	}
	options := tui.Options{BuildInfo: info, CommandHelp: renderHelp(theme.New(false, false)), ColorProfile: outputProfile(color, stdout)}
	if err := tui.Run(ctx, stdin, stdout, options); err != nil {
		fmt.Fprintf(stderr, "terminal console: %v\n", err)
		return 1
	}
	return 0
}

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/colorprofile"

	"github.com/papilio-platform/Node/internal/buildinfo"
	"github.com/papilio-platform/Node/internal/tui"
)

// versionOutput is the CLI's JSON wire contract, independent of internal records.
type versionOutput struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	Modified  bool   `json:"modified"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

func runVersion(args []string, stdout, stderr io.Writer, info buildinfo.Info, color string) int {
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
		styles := newPalette(colorEnabled(color, stdout))
		output := &colorprofile.Writer{Forward: stdout, Profile: outputProfile(color, stdout)}
		_, err = fmt.Fprintf(output, "%s %s (%s %s/%s)\n",
			styles.brand.Render("PapilioVPN Node"), styles.accent.Render(info.Version), info.GoVersion, info.OS, info.Arch)
	}
	if err != nil {
		fmt.Fprintf(stderr, "write version: %v\n", err)
		return 1
	}
	return 0
}

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
	options := tui.Options{BuildInfo: info, CommandHelp: renderHelp(newPalette(false)), ColorProfile: outputProfile(color, stdout)}
	if err := tui.Run(ctx, stdin, stdout, options); err != nil {
		fmt.Fprintf(stderr, "terminal console: %v\n", err)
		return 1
	}
	return 0
}

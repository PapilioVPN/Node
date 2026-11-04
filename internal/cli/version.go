package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/charmbracelet/colorprofile"

	"github.com/papilio-platform/Node/internal/buildinfo"
	"github.com/papilio-platform/Node/internal/terminal/theme"
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
		styles := theme.New(colorEnabled(color, stdout), false)
		output := &colorprofile.Writer{Forward: stdout, Profile: outputProfile(color, stdout)}
		_, err = fmt.Fprintf(output, "%s %s (%s %s/%s)\n",
			styles.Brand.Render("PapilioVPN Node"), styles.Accent.Render(info.Version), info.GoVersion, info.OS, info.Arch)
	}
	if err != nil {
		fmt.Fprintf(stderr, "write version: %v\n", err)
		return 1
	}
	return 0
}

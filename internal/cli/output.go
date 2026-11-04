package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"

	"github.com/papilio-platform/Node/internal/terminal/theme"
)

func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}

func colorEnabled(mode string, output io.Writer) bool {
	enabled := mode == "always"
	if mode == "auto" {
		// NO_COLOR specifies any nonempty value, including "0" or "false".
		enabled = isTerminal(output) && os.Getenv("TERM") != "dumb" && os.Getenv("NO_COLOR") == ""
	}
	return enabled
}

func outputProfile(mode string, output io.Writer) colorprofile.Profile {
	profile := colorprofile.NoTTY
	if colorEnabled(mode, output) {
		profile = max(colorprofile.ANSI, colorprofile.Env(os.Environ()))
		if mode == "always" {
			profile = colorprofile.TrueColor
		}
	}
	return profile
}

func renderHelp(p theme.Styles) string {
	return fmt.Sprintf(`%s  %s
The PapilioVPN Node command line.

%s
  node [--color auto|always|never] <command>

%s
  help              Show available commands.
  version [--json]  Print binary version and build metadata.
  tui               Open the interactive terminal console.

%s
  --color <mode>    auto (default), always or never; before the command.
  -h, --help        Show help. Use <command> --help for command options.

%s
`, p.Brand.Render("PapilioVPN Node"), p.Accent.Render("[ DEVELOPMENT ]"),
		p.Brand.Render("Usage:"), p.Brand.Render("Commands:"), p.Brand.Render("Options:"),
		p.Muted.Render("Engine management is not available in this development build."))
}

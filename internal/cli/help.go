package cli

import (
	"fmt"

	"github.com/papilio-platform/Node/internal/terminal/theme"
)

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

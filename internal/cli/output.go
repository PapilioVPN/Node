package cli

import (
	"fmt"
	"io"
	"os"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
)

// palette is local to terminal presentation. Plain mode removes text attributes
// as well as colors, while keeping spacing and textual selection markers.
type palette struct {
	brand, accent, muted lipgloss.Style
}

func newPalette(color bool) palette {
	p := palette{}
	if color {
		cyan, purple := "#0E7490", "#7C3AED"
		p.brand = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(cyan))
		p.accent = lipgloss.NewStyle().Foreground(lipgloss.Color(purple))
		// Default terminal foreground keeps secondary text readable on both themes.
		p.muted = lipgloss.NewStyle().Italic(true)
	}
	return p
}

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

func renderHelp(p palette) string {
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
`, p.brand.Render("PapilioVPN Node"), p.accent.Render("[ DEVELOPMENT ]"),
		p.brand.Render("Usage:"), p.brand.Render("Commands:"), p.brand.Render("Options:"),
		p.muted.Render("Engine management is not available in this development build."))
}

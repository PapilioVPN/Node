package cli

import (
	"io"
	"os"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
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

// Package tui implements the interactive Node terminal console. It owns terminal
// rendering and navigation, independently of command-line parsing and exit codes.
package tui

import (
	"context"
	"io"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/papilio-platform/Node/internal/buildinfo"
)

// Options supplies the current binary's content and terminal presentation policy.
// CommandHelp is plain command discovery text supplied by the CLI that owns it.
type Options struct {
	BuildInfo    buildinfo.Info
	CommandHelp  string
	ColorProfile colorprofile.Profile
}

// Run owns an interactive session until quit, cancellation or failure, then
// restores terminal state. The caller owns the streams and must supply terminal
// input/output, never redirected streams or TERM=dumb.
func Run(ctx context.Context, input io.Reader, output io.Writer, options Options) error {
	program := tea.NewProgram(newConsole(options.BuildInfo, options.CommandHelp, options.ColorProfile > colorprofile.NoTTY),
		tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output),
		tea.WithColorProfile(options.ColorProfile))
	_, err := program.Run()
	return err
}

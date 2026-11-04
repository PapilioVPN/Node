// Package theme defines Papilio's terminal presentation styles.
package theme

import "charm.land/lipgloss/v2"

// Styles gives CLI output and TUI views the same visual identity.
type Styles struct {
	Brand, Accent, Muted, Selected lipgloss.Style
}

// New builds styles for an explicit color and background policy. Disabling color
// removes text attributes as well as colors; it leaves text and spacing intact.
func New(color, dark bool) Styles {
	p := Styles{}
	if !color {
		return p
	}
	cyan, purple := "#0E7490", "#7C3AED"
	if dark {
		cyan, purple = "#67E8F9", "#C4B5FD"
	}
	p.Brand = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(cyan))
	p.Accent = lipgloss.NewStyle().Foreground(lipgloss.Color(purple))
	// Keep the terminal's default foreground for readable secondary text.
	p.Muted = lipgloss.NewStyle().Italic(true)
	p.Selected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#7C3AED"))
	return p
}

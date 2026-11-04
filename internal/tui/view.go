package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// palette is local to terminal presentation. Plain mode removes text attributes
// as well as colors, while keeping spacing and textual selection markers.
type palette struct {
	brand, accent, muted, selected lipgloss.Style
}

func newPalette(color, dark bool) palette {
	p := palette{}
	if color {
		cyan, purple := "#0E7490", "#7C3AED"
		if dark {
			cyan, purple = "#67E8F9", "#C4B5FD"
		}
		p.brand = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(cyan))
		p.accent = lipgloss.NewStyle().Foreground(lipgloss.Color(purple))
		// Default terminal foreground keeps secondary text readable on both themes.
		p.muted = lipgloss.NewStyle().Italic(true)
		p.selected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#7C3AED"))
	}
	return p
}

func (m *console) refresh() {
	p := newPalette(m.color, m.dark)
	var content string
	switch {
	case m.help:
		content = p.brand.Render("KEYBOARD SHORTCUTS") + `

Tab / Shift+Tab    Next / previous page
Left / Right      Previous / next page (h / l)
1 / 2 / 3         Overview / build / commands
Up / Down         Scroll one line (k / j)
PgUp / PgDown     Scroll one page
Home / End        Start / end of page (g / G)
?                 Toggle this help
Esc               Close help, otherwise quit
q / Ctrl+C        Quit and restore the terminal

Resize the terminal at any time. Use node --color=never tui for a plain console.
This console inspects the current binary; it does not manage a running service.`
	case m.page == 0:
		content = p.brand.Render("YOUR NODE, IN FOCUS") + "\n\n" +
			"Welcome to the PapilioVPN Node console.\n\n" +
			p.accent.Render("AVAILABLE IN THIS BUILD") + "\n" +
			"  Build     Inspect version, revision and target platform.\n" +
			"  Commands  Find real commands and script-friendly output.\n\n" +
			p.accent.Render("RUNTIME AVAILABILITY") + "\n" +
			"Engine management and the network control API are not available in this development build.\n" +
			"No engine or service is queried by this console.\n\n" +
			p.muted.Render("Press 2 for build details, 3 for commands or ? for keyboard help.")
	case m.page == 1:
		revision := m.info.Revision
		if revision == "" {
			revision = "not embedded"
		}
		content = fmt.Sprintf("%s\n\nVersion      %s\nGo           %s\nPlatform     %s/%s\nRevision     %s\nModified     %t\n\n%s\n%s",
			p.brand.Render("BINARY IDENTITY"), m.info.Version, m.info.GoVersion, m.info.OS, m.info.Arch, revision, m.info.Modified,
			p.accent.Render("ABOUT THIS METADATA"),
			"Values describe this binary at build time. Modified records source changes at build time. An absent revision is not an engine health result.\n\nFor automation, run: node version --json")
	default:
		content = m.commands + "\n" + p.accent.Render("SCRIPTING") + "\n" +
			"node version --json\n\nJSON never contains colors or terminal control codes. Global options precede the command.\n\n" +
			"node --color=always help\nnode --color=never tui\n\nNO_COLOR with any nonempty value disables automatic styling. The explicit --color option takes precedence."
	}
	m.viewport.SetContent(ansi.Wrap(content, m.viewport.Width(), ""))
}

func (m console) View() tea.View {
	p := newPalette(m.color, m.dark)
	var content string
	if m.width == 0 || m.height == 0 {
		content = ""
	} else if m.width < 48 || m.height < 16 {
		content = lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(
			"PapilioVPN Node\nResize to at least 48 x 16.\nq / Ctrl+C to quit.")
	} else {
		width := m.width - 4
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(width)
		if m.color {
			box = box.BorderForeground(p.accent.GetForeground())
		}
		badge := "DEVELOPMENT BUILD"
		if m.width < 76 {
			badge = "DEV"
		}
		brand := p.brand.Render("◈ PapilioVPN Node")
		gap := strings.Repeat(" ", max(1, width-4-lipgloss.Width(brand)-len(badge)))
		header := box.Render(brand + gap + p.accent.Render(badge) + "\n" + p.muted.Render("Your node. In focus."))
		var tabs []string
		for index, name := range []string{"Overview", "Build", "Commands"} {
			label := fmt.Sprintf(" %d %s ", index+1, name)
			if index == m.page && !m.help {
				label = p.selected.Render("[" + strings.TrimSpace(label) + "]")
			}
			tabs = append(tabs, label)
		}
		footer := fmt.Sprintf("Tab pages · ↑↓ scroll · ? help · q quit   %3.0f%%", m.viewport.ScrollPercent()*100)
		if m.width < 76 {
			footer = "Tab pages · ↑↓ scroll · ? help · q quit"
		}
		content = lipgloss.NewStyle().Margin(1, 2).Render(strings.Join([]string{
			header, strings.Join(tabs, " "), box.Render(m.viewport.View()), p.muted.Render(footer),
		}, "\n\n"))
	}
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}

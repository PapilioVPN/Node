package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/papilio-platform/Node/internal/terminal/theme"
)

func (m console) View() tea.View {
	p := theme.New(m.color, m.dark)
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
			box = box.BorderForeground(p.Accent.GetForeground())
		}
		badge := "DEVELOPMENT BUILD"
		if m.width < 76 {
			badge = "DEV"
		}
		brand := p.Brand.Render("◈ PapilioVPN Node")
		gap := strings.Repeat(" ", max(1, width-4-lipgloss.Width(brand)-len(badge)))
		header := box.Render(brand + gap + p.Accent.Render(badge) + "\n" + p.Muted.Render("Your node. In focus."))
		var tabs []string
		for index, name := range []string{"Overview", "Build", "Commands"} {
			label := fmt.Sprintf(" %d %s ", index+1, name)
			if index == m.page && !m.help {
				label = p.Selected.Render("[" + strings.TrimSpace(label) + "]")
			}
			tabs = append(tabs, label)
		}
		footer := fmt.Sprintf("Tab pages · ↑↓ scroll · ? help · q quit   %3.0f%%", m.viewport.ScrollPercent()*100)
		if m.width < 76 {
			footer = "Tab pages · ↑↓ scroll · ? help · q quit"
		}
		content = lipgloss.NewStyle().Margin(1, 2).Render(strings.Join([]string{
			header, strings.Join(tabs, " "), box.Render(m.viewport.View()), p.Muted.Render(footer),
		}, "\n\n"))
	}
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}

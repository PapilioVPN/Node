package tui

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/papilio-platform/Node/internal/buildinfo"
)

func TestConsoleLayoutAndThemes(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {1, 1}, {32, 8}, {48, 16}, {64, 20}, {80, 24}, {112, 36}, {240, 80}} {
		for _, colored := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/color=%t/dark=%t", size[0], size[1], colored, dark), func(t *testing.T) {
					m := newConsole(buildinfo.Info{Revision: strings.Repeat("a", 40)}, "node help\nnode version --json", colored)
					m = updateConsole(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					background := color.White
					if dark {
						background = color.Black
					}
					m = updateConsole(t, m, tea.BackgroundColorMsg{Color: background})
					for page := range 4 {
						m.page, m.help = page%3, page == 3
						m.refresh()
						view := m.View()
						if width, height := lipgloss.Size(view.Content); width > size[0] || height > size[1] && size[1] > 0 {
							t.Errorf("page %d overflows %dx%d: %dx%d", page, size[0], size[1], width, height)
						}
						if !colored && strings.Contains(view.Content, "\x1b") {
							t.Error("plain view contains terminal styling")
						}
						if !view.AltScreen {
							t.Error("console does not request an alternate screen")
						}
					}
				})
			}
		}
	}
}

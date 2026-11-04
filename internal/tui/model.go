package tui

import (
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/papilio-platform/Node/internal/buildinfo"
	"github.com/papilio-platform/Node/internal/terminal/theme"
	"github.com/papilio-platform/Node/internal/tui/pages"
)

type console struct {
	info          buildinfo.Info
	commands      string
	width, height int
	page          int
	help          bool
	color, dark   bool
	viewport      viewport.Model
}

func newConsole(info buildinfo.Info, commands string, color bool) console {
	view := viewport.New()
	view.SoftWrap = true
	view.FillHeight = true
	return console{info: info, commands: commands, color: color, dark: true, viewport: view}
}

func (m console) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

func (m console) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var command tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Cap presentation allocation even on an unusually large terminal.
		m.width, m.height = min(max(msg.Width, 0), 112), min(max(msg.Height, 0), 60)
		m.viewport.SetWidth(max(1, m.width-8))
		m.viewport.SetHeight(max(1, m.height-13))
		m.refresh()
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.refresh()
	case tea.KeyPressMsg:
		previous, help := m.page, m.help
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			if !m.help {
				return m, tea.Quit
			}
			m.help = false
		case "?":
			m.help = !m.help
		case "tab", "right", "l":
			m.page, m.help = (m.page+1)%3, false
		case "shift+tab", "left", "h":
			m.page, m.help = (m.page+2)%3, false
		case "1", "2", "3":
			m.page, m.help = int(msg.Code-'1'), false
		case "home", "g":
			m.viewport.GotoTop()
		case "end", "G":
			m.viewport.GotoBottom()
		default:
			m.viewport, command = m.viewport.Update(msg)
		}
		if m.page != previous || m.help != help {
			m.viewport.GotoTop()
			m.refresh()
		}
	}
	return m, command
}

func (m *console) refresh() {
	styles := theme.New(m.color, m.dark)
	var content string
	switch {
	case m.help:
		content = pages.Shortcuts(styles)
	case m.page == 0:
		content = pages.Overview(styles)
	case m.page == 1:
		content = pages.Build(m.info, styles)
	default:
		content = pages.Commands(m.commands, styles)
	}
	m.viewport.SetContent(ansi.Wrap(content, m.viewport.Width(), ""))
}

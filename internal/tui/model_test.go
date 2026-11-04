package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/papilio-platform/Node/internal/buildinfo"
)

func updateConsole(t *testing.T, model console, message tea.Msg) console {
	t.Helper()
	updated, _ := model.Update(message)
	return updated.(console)
}

func TestConsoleNavigationAndScrolling(t *testing.T) {
	m := newConsole(buildinfo.Info{Version: "test-version", Revision: "test-revision"}, "node help\n"+strings.Repeat("commands\n", 30), false)
	m = updateConsole(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(m.View().Content, "YOUR NODE, IN FOCUS") {
		t.Fatal("initial view does not describe actual capabilities")
	}
	m = updateConsole(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	for _, value := range []string{"BINARY IDENTITY", "test-version", "test-revision"} {
		if !strings.Contains(m.viewport.GetContent(), value) {
			t.Errorf("missing build information %q", value)
		}
	}
	m = updateConsole(t, m, tea.KeyPressMsg{Code: '3'})
	m = updateConsole(t, m, tea.KeyPressMsg{Code: tea.KeyEnd})
	if !m.viewport.AtBottom() || m.viewport.YOffset() == 0 {
		t.Fatal("command reference cannot scroll to the end")
	}
	m = updateConsole(t, m, tea.KeyPressMsg{Code: tea.KeyHome})
	if !m.viewport.AtTop() {
		t.Fatal("home does not return to the start")
	}
	m = updateConsole(t, m, tea.KeyPressMsg{Code: '?'})
	if !strings.Contains(m.View().Content, "KEYBOARD SHORTCUTS") {
		t.Fatal("keyboard help is not accessible")
	}
	m = updateConsole(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.help || m.page != 2 {
		t.Fatal("escape does not restore the selected page")
	}
	m = updateConsole(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.page != 0 || !m.viewport.AtTop() {
		t.Fatal("tab does not wrap and reset scrolling")
	}
	m = updateConsole(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.page != 2 {
		t.Fatal("shift-tab does not wrap to the previous page")
	}
	for _, key := range []tea.KeyPressMsg{{Code: 'q'}, {Code: 'c', Mod: tea.ModCtrl}, {Code: tea.KeyEscape}} {
		_, command := m.Update(key)
		if command == nil {
			t.Fatalf("%s has no exit command", key.String())
		}
		if _, ok := command().(tea.QuitMsg); !ok {
			t.Errorf("%s does not quit", key.String())
		}
	}
}

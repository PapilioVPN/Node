package pages

import "github.com/papilio-platform/Node/internal/terminal/theme"

// Commands adds scripting guidance to command help supplied by the CLI.
func Commands(help string, p theme.Styles) string {
	return help + "\n" + p.Accent.Render("SCRIPTING") + "\n" +
		"node version --json\n\nJSON never contains colors or terminal control codes. Global options precede the command.\n\n" +
		"node --color=always help\nnode --color=never tui\n\nNO_COLOR with any nonempty value disables automatic styling. The explicit --color option takes precedence."
}

// Shortcuts renders the console's keyboard reference.
func Shortcuts(p theme.Styles) string {
	return p.Brand.Render("KEYBOARD SHORTCUTS") + `

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
}

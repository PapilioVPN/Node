// Package pages renders console content without terminal I/O or event handling.
package pages

import (
	"fmt"

	"github.com/papilio-platform/Node/internal/buildinfo"
	"github.com/papilio-platform/Node/internal/terminal/theme"
)

// Overview describes the capabilities available in this build.
func Overview(p theme.Styles) string {
	return p.Brand.Render("YOUR NODE, IN FOCUS") + "\n\n" +
		"Welcome to the PapilioVPN Node console.\n\n" +
		p.Accent.Render("AVAILABLE IN THIS BUILD") + "\n" +
		"  Build     Inspect version, revision and target platform.\n" +
		"  Commands  Find real commands and script-friendly output.\n\n" +
		p.Accent.Render("RUNTIME AVAILABILITY") + "\n" +
		"Engine management and the network control API are not available in this development build.\n" +
		"No engine or service is queried by this console.\n\n" +
		p.Muted.Render("Press 2 for build details, 3 for commands or ? for keyboard help.")
}

// Build renders the Node binary's metadata, including absent revision information.
func Build(info buildinfo.Info, p theme.Styles) string {
	revision := info.Revision
	if revision == "" {
		revision = "not embedded"
	}
	return fmt.Sprintf("%s\n\nVersion      %s\nGo           %s\nPlatform     %s/%s\nRevision     %s\nModified     %t\n\n%s\n%s",
		p.Brand.Render("BINARY IDENTITY"), info.Version, info.GoVersion, info.OS, info.Arch, revision, info.Modified,
		p.Accent.Render("ABOUT THIS METADATA"),
		"Values describe this binary at build time. Modified records source changes at build time. An absent revision is not an engine health result.\n\nFor automation, run: node version --json")
}

# Contributing to PapilioVPN Node

Use [issues](https://github.com/papilio-platform/Node/issues) for reproducible
problems and [pull requests](https://github.com/papilio-platform/Node/pulls) for
focused changes. Describe the resulting behavior and how you verified it.

## Before submitting

- Use Go 1.26 or newer and keep changes focused on a coherent responsibility.
- Preserve typed contracts, cancellation, error meaning and resource ownership.
- Add meaningful behavior tests for implemented capabilities. Identify whether
  verification uses controlled inputs or real isolated engine/OS integration.
- Keep code, comments and commit/PR metadata in English. Update both
  [English](README.md) and [Persian](README.fa.md) operational instructions when
  their corresponding behavior changes.
- Keep credentials, runtime configuration, generated binaries and private work
  plans out of commits.

Run:

```bash
gofmt -w cmd internal tests
go mod tidy
go mod verify
go vet ./...
go test -race -count=1 ./...
go build -trimpath -o bin/node ./cmd/node
```

The [CI workflow](.github/workflows/ci.yml) declares the pinned compiler/scanner
and verifies native Linux amd64 and arm64 builds.
Linux tests open isolated pseudo-terminals to verify keyboard navigation,
resize, cancellation, signals and restoration. For terminal changes, also inspect
the real `node tui` in light/dark themes, a small window and colorless mode.
Keep JSON output free of all terminal styling and control codes.

## Reporting a problem

Include `node version --json`, the command or operation, expected/actual results
and steps to reproduce. Remove secrets and private user data from any examples.
Distinguish a confirmed result from an untested assumption.

## Code organization

```text
cmd/node/                 Process entrypoint and composition
internal/buildinfo/       Binary metadata, independent of presentation
internal/cli/             Command parsing, help, JSON and exit codes
internal/terminal/theme/  Shared terminal styles, with no environment or I/O
internal/tui/             Interactive session, navigation and views
internal/tui/pages/       Pure information and command-reference content
tests/integration/        Real cross-package and OS integration tests
```

Keep ordinary `*_test.go` files beside the package they verify, as in the
[Go module layout guide](https://go.dev/doc/modules/layout). Use `package name_test`
for external contract tests; same-package tests can verify internal policies or
event models. Test files are excluded from ordinary builds. Add responsibility
packages when actual behavior needs them, and keep dependencies acyclic.
The CLI launches the TUI with explicit content/presentation options; the TUI does
not parse CLI arguments or decide process exit codes.
Within the CLI, `cli.go` owns root dispatch, `version.go` owns version output,
`tui.go` owns interactive entry, `help.go` owns command discovery and
`terminal.go` owns terminal/color policy.
Both adapters use the terminal theme; they own color policy and background
detection respectively. The theme only constructs Lip Gloss styles from those
explicit inputs and does not import either adapter.
The TUI model selects pages and owns viewport state; `view.go` lays out the frame
and `tui.go` runs the Bubble Tea session. Page rendering consumes explicit build
metadata, command text and styles without importing event, process or CLI code.

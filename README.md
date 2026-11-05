<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/banner.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/banner-light.png">
    <img alt="PapilioVPN Node — the service for managed VPN nodes" src="assets/banner-light.png" width="100%">
  </picture>
</p>

<h1 align="center">PapilioVPN Node</h1>

<p align="center">The Go service for nodes managed by the PapilioVPN platform.</p>

<p align="center">
  <a href="https://github.com/papilio-platform/Node/actions/workflows/ci.yml"><img alt="CI status" src="https://github.com/papilio-platform/Node/actions/workflows/ci.yml/badge.svg?branch=main"></a>
  <a href="go.mod"><img alt="Go 1.27.3 or newer" src="https://img.shields.io/badge/Go-1.27.3%2B-00ADD8?style=flat-square&amp;logo=go&amp;logoColor=white"></a>
  <img alt="Status: early development" src="https://img.shields.io/badge/status-early_development-8B5CF6?style=flat-square">
</p>

<p align="center"><strong>English</strong> · <a href="README.fa.md" lang="fa">فارسی</a></p>

<p align="center">
  <a href="#current-capabilities">Capabilities</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#development">Development</a> ·
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

---

## Current capabilities

This revision provides the executable foundation and command-line interface.
The internal Xray adapter provides native configuration validation and owned
process startup/shutdown. Engine operations are not exposed as CLI commands
or network control endpoints yet.
The internal instance service coordinates lifecycle operations and reports
configuration revisions, native version and confirmed process outcomes.

| Available now | Behavior |
| --- | --- |
| Command discovery | Help lists the commands this binary actually provides. |
| Terminal console | Branded, keyboard-driven overview, build details and command reference with scrolling and responsive layout. |
| Version inspection | Human-readable version, Go version and target platform. |
| Structured build metadata | JSON includes revision and working-tree state when embedded by Go. |
| Native configuration checks | Bounded Xray validation with cancellation and private diagnostics. See [tested scope and limits](docs/engines/xray.md). |
| Owned Xray processes | Native API readiness through a private Unix socket, bounded shutdown and owned-resource cleanup. Internal adapter only; see [lifecycle limits](docs/engines/xray.md#owned-process-contract). |
| Instance lifecycle | Typed state/revisions, native version and exit/cleanup outcomes. Bounded start/stop/restart with explicit recovery; real-engine tests cover local forwarding, crash recovery and lifetime shutdown. Status reads perform no engine I/O; see [instance contracts](docs/instances.md). |
| Engine console logs | Opt-in bounded replay/live reads with resumable cursors, explicit retention gaps and per-run identity. Xray console capture is verified across real traffic and restart; native file/disabled settings remain authoritative. See [log contracts](docs/engine-logs.md). |
| Engine configuration | Sensitive desired-source reads, effective native validation and revision-conditional full replacement. Unchanged healthy input reuses the process; failed replacement retains actual desired/applied history. Full changes require restart; see [configuration contracts](docs/configuration.md). [Empty managed Shadowsocks2022 AES membership](docs/engines/xray.md#empty-shadowsocks2022-membership) starts with zero real users. |
| Live users | Focused reads and revision-conditional create/replace/remove for VMess, VLESS (including Vision over verified TLS 1.3), Trojan and legacy Shadowsocks AEAD accounts, with real accepted/denied traffic checks. Live edits reset on restart; removal preserves established streams. See [user contracts and tested scope](docs/users.md). |
| Linux verification | CI builds and tests natively on amd64 and arm64, including checksum-pinned Xray validation and process tests. |

## Quick start

Install [Go 1.27.3 or newer](https://go.dev/dl/), then build from source:

```bash
git clone https://github.com/papilio-platform/Node.git
cd Node
go build -trimpath -o bin/node ./cmd/node
./bin/node help
./bin/node version
./bin/node version --json
./bin/node tui
```

This builds the development CLI. It does not install a system service or change
network configuration. Versioned installation packages are not published yet.

## Command reference

| Command | Result |
| --- | --- |
| `node help` | Show usage and available commands. `-h` and `--help` are also accepted. |
| `node version` | Print a readable version and platform description. |
| `node version --json` | Print `version`, `revision`, `modified`, `go_version`, `os` and `arch`. |
| `node version --help` | Show version command options. |
| `node tui` | Open the interactive console in a terminal. |
| `node tui --help` | Show console requirements and navigation. |
| `node --color=auto\|always\|never <command>` | Select styling. Global options go before the command. |

### Interactive console

Run `node tui` to browse **Overview**, **Build** and **Commands**. The console
inspects the current binary; it does not query a running Node service or manage
engines. It uses a full-screen terminal view and restores the previous screen,
cursor and input modes when it closes.

| Keys | Action |
| --- | --- |
| `Tab` / `Shift+Tab`, `←` / `→` or `h` / `l` | Switch pages. |
| `1` / `2` / `3` | Overview, build details or commands. |
| `↑` / `↓` or `k` / `j` | Scroll the page. |
| `PgUp` / `PgDown`, `Home` / `End` | Page through content or jump to its start/end. |
| `?` | Toggle keyboard help. |
| `Esc` | Close help; otherwise quit. |
| `q` / `Ctrl+C` | Quit the console. |

The layout adapts to window size and terminal light/dark background reports.
Below **48 columns × 16 rows**, it shows a resize hint and keeps exit keys active.
The console requires terminal input and output; redirected streams and
`TERM=dumb` receive a usage error without entering interactive mode.

Human output uses color automatically on a capable terminal. Any nonempty
`NO_COLOR` disables automatic styling. `--color=never` removes styling;
`--color=always` explicitly enables it, including for redirected human output.
The explicit option takes precedence over `NO_COLOR`. Interactive screen-control
codes are still necessary in the colorless TUI. JSON is always unstyled and
contains no terminal control codes, regardless of the color option.

Version output uses Go's embedded module version, which can be a commit-based
pseudo-version for a development build. It falls back to `devel` when no module
version is available. A build without VCS metadata reports an empty `revision`;
the binary does not invoke Git at runtime. `modified` describes the source state
at build time, not files on the machine running the binary.

Exit codes are **0** for success, **1** for execution/output failure and **2** for invalid
usage. Invalid commands produce a diagnostic on stderr.

## Development

The terminal interface uses pinned [Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Bubbles](https://github.com/charmbracelet/bubbles) and
[Lip Gloss](https://github.com/charmbracelet/lipgloss) dependencies. Run the same
core checks used in CI:

```bash
gofmt -w cmd internal tests
go mod tidy
go mod verify
go vet ./...
go test -race -count=1 ./...
go build -trimpath -o bin/node ./cmd/node
```

CI also checks module/formatting consistency and reachable vulnerabilities with
a pinned `govulncheck`. Compiler and scanner versions are declared in the
[CI workflow](.github/workflows/ci.yml). Race tests require a supported C toolchain.
Linux tests exercise real pseudo-terminals for navigation, resize, color policy,
cancellation, signals and terminal restoration.
Real engine tests require the pinned Xray executable; see the
[engine verification instructions](CONTRIBUTING.md#native-xray-tests).

## Contributing

Read the [contribution guide](CONTRIBUTING.md),
[report an issue](https://github.com/papilio-platform/Node/issues), or
[open a pull request](https://github.com/papilio-platform/Node/pulls).
Include the binary metadata and a reproducible example when reporting a problem.
Keep English and Persian instructions consistent when changing behavior.

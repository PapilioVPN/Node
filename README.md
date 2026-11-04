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
  <a href="go.mod"><img alt="Go 1.26 or newer" src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?style=flat-square&amp;logo=go&amp;logoColor=white"></a>
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
Engine management and the network control API are not available yet.

| Available now | Behavior |
| --- | --- |
| Command discovery | Help lists the commands this binary actually provides. |
| Version inspection | Human-readable version, Go version and target platform. |
| Structured build metadata | JSON includes revision and working-tree state when embedded by Go. |
| Linux verification | CI builds and tests the CLI natively on amd64 and arm64. |

## Quick start

Install [Go 1.26 or newer](https://go.dev/dl/), then build from source:

```bash
git clone https://github.com/papilio-platform/Node.git
cd Node
go build -trimpath -o bin/node ./cmd/node
./bin/node help
./bin/node version
./bin/node version --json
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

Version output uses Go's embedded module version, which can be a commit-based
pseudo-version for a development build. It falls back to `devel` when no module
version is available. A build without VCS metadata reports an empty `revision`;
the binary does not invoke Git at runtime. `modified` describes the source state
at build time, not files on the machine running the binary.

Exit codes are **0** for success, **1** for output failure and **2** for invalid
usage. Invalid commands produce a diagnostic on stderr.

## Development

The current executable uses the Go standard library and has no third-party
runtime dependencies. Run the same core checks used in CI:

```bash
gofmt -w cmd internal
go mod tidy
go mod verify
go vet ./...
go test -race -count=1 ./...
go build -trimpath -o bin/node ./cmd/node
```

CI also checks module/formatting consistency and reachable vulnerabilities with
a pinned `govulncheck`. Compiler and scanner versions are declared in the
[CI workflow](.github/workflows/ci.yml). Race tests require a supported C toolchain.

## Contributing

Read the [contribution guide](CONTRIBUTING.md),
[report an issue](https://github.com/papilio-platform/Node/issues), or
[open a pull request](https://github.com/papilio-platform/Node/pulls).
Include the binary metadata and a reproducible example when reporting a problem.
Keep English and Persian instructions consistent when changing behavior.

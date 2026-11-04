# Contributing to PapilioVPN Node

Use [issues](https://github.com/papilio-platform/Node/issues) for reproducible
problems and [pull requests](https://github.com/papilio-platform/Node/pulls) for
focused changes. Describe the resulting behavior and how you verified it.

## Before submitting

- Use Go 1.27.3 or newer and keep changes focused on a coherent responsibility.
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

## Native Xray tests

CI acquires Xray v26.10.31 for the runner's architecture and checks the official
release archive SHA256 before extracting the executable. To run the same tests
on Linux amd64 or arm64, choose an unused destination:

```bash
xray_test_root=$(mktemp -d)
bash scripts/ci/install-xray.sh "$xray_test_root/engine"
PAPILIO_TEST_XRAY="$xray_test_root/engine/xray" go test -race -count=1 ./...
```

The downloader refuses to replace an existing destination. Remove your temporary
test directory when finished. Alternatively, set `PAPILIO_TEST_XRAY` to an absolute
path to an already verified copy of that exact engine version. Without this
variable, local native-engine tests skip; controlled process tests still run.
CI always supplies it, and a missing or incorrect engine fails verification.
Tests use isolated temporary files and private socket paths, with no host routing
changes or customer configuration. Native validation runs under your privileges
and is not a sandbox. Use only the controlled test fixtures.

To measure the actual native check/start/API-ready/stop cycle:

```bash
PAPILIO_TEST_XRAY="$xray_test_root/engine/xray" go test -run '^$' -bench '^BenchmarkXrayLifecycle$' -benchtime=5x -count=1 ./tests/integration
```

Record compiler, engine version, host and workload with any results. Allocation
figures describe Node/client allocations, not the external engine's memory.
This benchmark does not measure traffic throughput or establish superiority
over another Node implementation.

To measure status reads over an actual ready Xray session, excluding startup:

```bash
PAPILIO_TEST_XRAY="$xray_test_root/engine/xray" go test -run '^$' -bench '^BenchmarkInstanceStatus$' -benchmem -count=1 ./tests/integration
```

The [instance acceptance checks](docs/instances.md#verification-and-cost) also
exercise local payload forwarding across three lifecycle cycles, native rejection,
startup listener conflicts, explicit recovery and process-lifetime shutdown.
The HTTP proxy is private to the test's Unix socket; its target is a loopback
fixture, avoiding a find-free-port/release/rebind setup. The conflict test owns
the occupied port throughout failure injection and deliberately releases it for
recovery. No test contacts an external destination.

To measure the complete application service over its actual native adapter:

```bash
PAPILIO_TEST_XRAY="$xray_test_root/engine/xray" go test -run '^$' -bench '^BenchmarkInstanceLifecycle$' -benchtime=5x -benchmem -count=1 ./tests/integration
```

The phase metrics `start-ready-ns/op` and `stop-cleanup-ns/op` include actual native
work; the standard total includes both application operations and outcome checks.
Successful API readiness is distinct from the upstream signal-handler timing
[limitation](docs/engines/xray.md#owned-process-contract). This benchmark requires
a clean native shutdown and fails on abnormal termination.

## Reporting a problem

Include `node version --json`, the command or operation, expected/actual results
and steps to reproduce. Remove secrets and private user data from any examples.
Distinguish a confirmed result from an untested assumption.

## Code organization

```text
cmd/node/                 Process entrypoint and composition
internal/buildinfo/       Binary metadata, independent of presentation
internal/cli/             Command parsing, help, JSON and exit codes
internal/cores/xray/      Native configuration, API readiness and owned processes
internal/instance/        Engine-independent lifecycle policy and observations
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
Within the Xray adapter, `configuration.go` owns the pure management projection,
`check.go` native validation, `api.go` the native client, `launch_linux.go` owned
startup and `process_linux.go` the running session/Wait/shutdown. Bounded output
retention stays in `diagnostics.go`. None imports presentation code.
Native adapters depend on the instance owner's typed contracts. The instance
service imports no engine implementation, process API or generated messages;
`service.go` owns state/snapshots, while `start.go` and `stop.go` own their respective
workflows. Unit tests use controlled contracts; integration tests compose the
same service with the real Xray launcher and verify actual process termination.

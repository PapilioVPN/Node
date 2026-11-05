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

To measure the explicit restart operation over a ready native instance:

```bash
PAPILIO_TEST_XRAY="$xray_test_root/engine/xray" go test -run '^$' -bench '^BenchmarkInstanceRestart$' -benchtime=5x -benchmem -count=1 ./tests/integration
```

Restart acceptance covers normal replacement, crash recovery, interrupted
replacement admission and actual local payload forwarding before/after restart.
Controlled `testing/synctest` cases also verify conflicting operations, deadlines,
late sessions and incomplete cleanup; race tests mix bounded concurrent lifecycle
calls and assert at most one owned execution.

## Engine log checks

The full suite includes bounded retention, replay/live gaps, reader cleanup,
framing and sensitive-output checks. With `PAPILIO_TEST_XRAY` set as above, it also
verifies real startup/access logs, forwarding across restart, file/disabled log
settings and post-exit replay. See [log contracts](docs/engine-logs.md).

```bash
go test ./internal/cores/xray -run '^$' -fuzz '^FuzzRuntimeLogFraming$' -fuzztime=5s -parallel=2
go test ./internal/cores/xray -run '^$' -bench '^BenchmarkRuntimeLogOutput$' -benchmem -count=3
```

This benchmark feeds 128 native-format lines per write through the production
runtime-output adapter, retaining 256 records at a 1024-byte line limit and a
4096-byte private diagnostic tail. Subtests use zero/two draining pull readers.
Record Go version, CPU and workload when comparing results. The measurement
excludes actual engine/pipe/network execution; it is not data-plane throughput.

## Configuration checks

The full native suite covers unchanged reuse, conditional configuration
replacement, invalid-source isolation, actual traffic changes, listener-conflict
failure and explicit recovery. Controlled concurrency tests verify scoped
admission and interruption during validation and replacement. See the
[configuration contract](docs/configuration.md).

```bash
PAPILIO_TEST_XRAY="$xray_test_root/engine/xray" go test -run '^$' -bench '^BenchmarkConfigurationUnchangedApply$' -benchtime=10x -benchmem -count=3 ./tests/integration
```

This measures capture/hash, admission and a native API probe over a ready
freedom-only Xray, checking the same PID and `Reused` result each time. It excludes
initial startup and actual forwarding throughput. Healthy unchanged application
executes no validation or startup subprocess; changed input is validated before
old cleanup and rechecked by native startup.

## User observation checks

The full native suite reads actual VMess/VLESS/Trojan account metadata, verifies
empty/missing/unsupported inbound failures, real response-byte limits and stale
execution rejection after restart. Controlled tests assert focused RPC counts
and that Stop cancels and joins active query access. See [user contracts](docs/users.md).

```bash
PAPILIO_TEST_XRAY="$xray_test_root/engine/xray" go test -run '^$' -bench '^BenchmarkUsersNativeQueries$' -benchtime=100x -benchmem -count=3 ./tests/integration
```

This measures the actual service/access/native RPC path on a ready Xray with two
VMess accounts. Single-user and count queries use one RPC each; a complete bounded
list uses count plus one native collection response, without per-user reads.
Measurements exclude initial startup and forwarding throughput; record Go/engine
versions, host and concurrent load before comparing results.

## Account mutation checks

The native suite uses isolated VMess/VLESS/Trojan client/server pairs to verify
create/replace/remove, accepted/denied traffic, sibling isolation, stale revision
rejection, live-edit loss after restart and established-stream preservation.
An acknowledgement fault injector runs actual native writes before returning an
error, then verifies explicit replay/recovery against real engine state.
Legacy Shadowsocks acceptance adds four AEAD methods, method/password rotation,
native Unicode identity, mixed-cipher users and actual accepted/denied HTTPS
payloads. VLESS Vision acceptance uses verified outer TLS 1.3 and rejects wrong
flow, name, trust and incompatible transport versions.

```bash
PAPILIO_TEST_XRAY="$xray_test_root/engine/xray" go test -run '^$' -bench '^BenchmarkAccountNativeMutations$' -benchtime=100x -benchmem -count=3 ./tests/integration
```

This measures unchanged Create and actual conditional Replace on a ready Trojan
inbound with 1 and 100 accounts. Inspection makes three bounded native reads;
unchanged input sends no write, replacement sends remove then add. Account setup
and engine startup are excluded. This is control-path cost, not throughput or
comparative performance; retain equivalent conditions when comparing results.
For legacy Shadowsocks, measure the same unchanged Create boundary with native
key derivation and a ready mixed-method inbound at 1 and 100 accounts:

```bash
PAPILIO_TEST_XRAY="$xray_test_root/engine/xray" go test -run '^$' -bench '^BenchmarkShadowsocksNativeInspection$' -benchtime=100x -benchmem -count=3 ./tests/integration
```

This excludes account setup and engine startup and retains three bounded native
reads. It measures Node/control cost, without claiming forwarding throughput.
The native credential-mapping fuzz target also runs with the same bounds in CI:

```bash
go test ./internal/cores/xray -run='^$' -fuzz='^FuzzAccountCredentialKey$' -fuzztime=5s -parallel=2
```

Empty managed Shadowsocks2022 acceptance checks actual zero membership and denied
server-key traffic without synthetic users, plus native conversion failure,
cancellation/reaping, source preservation and private-file cleanup. Its native
preparation fuzz target runs in CI:

```bash
go test ./internal/cores/xray -run='^$' -fuzz='^FuzzManaged2022Preparation$' -fuzztime=5s -parallel=2
PAPILIO_TEST_XRAY="$xray_test_root/engine/xray" go test -run '^$' -bench '^BenchmarkSS2022ManagedValidation$' -benchtime=20x -benchmem -count=3 ./tests/integration
```

This benchmark includes native conversion, protobuf preparation, native validation
and temporary-file cleanup. Allocations describe the Node process, not child
engine memory/CPU. It does not measure readiness or forwarding throughput.

## Reporting a problem

Include `node version --json`, the command or operation, expected/actual results
and steps to reproduce. Remove secrets and private user data from any examples.
Distinguish a confirmed result from an untested assumption.

## Code organization

```text
cmd/node/                 Process entrypoint and composition
internal/buildinfo/       Binary metadata, independent of presentation
internal/cli/             Command parsing, help, JSON and exit codes
internal/configuration/   Engine-source validation, reads and conditional apply
internal/cores/xray/      Native configuration, API readiness and owned processes
internal/enginelog/       Bounded engine-output retention and pull subscriptions
internal/instance/        Engine-independent lifecycle policy and observations
internal/terminal/theme/  Shared terminal styles, with no environment or I/O
internal/tui/             Interactive session, navigation and views
internal/tui/pages/       Pure information and command-reference content
internal/users/           Execution-scoped user queries and conditional commands
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
`logs.go` frames runtime console output and maps native source/severity into the
engine-independent log buffer. `enginelog/buffer.go` owns retention/admission;
`subscription.go` owns cancellable cursor reads. Neither performs native I/O or
launches subscriber goroutines. The composition owns buffer lifetime across runs.
Native adapters depend on the instance owner's typed contracts. The instance
service imports no engine implementation, process API or generated messages;
`service.go` owns state/snapshots, while `start.go` and `stop.go` own their respective
workflows. Unit tests use controlled contracts; integration tests compose the
same service with the real Xray launcher and verify actual process termination.
`restart.go` coordinates explicit restart and its recovery result. Private
lifecycle phases require the replacement identity; public callers cannot bypass
admission.
Restart shares the existing start/stop execution instead of constructing another
engine owner or duplicating native lifecycle code. `replacement.go` owns scoped
replacement admission, activation and joined release, shared by restart and
configuration application. Configuration declarations/read admission stay in
`service.go`; `validate.go` and `apply.go` own their respective workflows.
Within Xray, `revision_linux.go` owns fresh candidate capture, original-source
reads and native effective-input validation/preparation shared with startup.
`instance/access.go` owns cancellable access to one ready execution; Stop joins
its release. `users/service.go` owns query contracts, input/admission bounds and
composition; `queries.go` owns focused reads. `xray/users_linux.go` owns generated
native query construction, byte/time bounds, metadata mapping and native errors.
No user/protocol methods were added to lifecycle `ISession`.

`users/accounts.go` owns single-account inputs, effects and consumed inspector/
writer contracts; `commands.go` coordinates conditional Create/Replace/Remove
through the existing instance access. Xray's `account_encoding.go` owns pure
native mapping, authentication keys and keyed revisions; `account_observation_linux.go`
executes bounded native inspection, `accounts_linux.go` checks its focused facts,
and `account_mutation_linux.go` executes individual native writes. Credentials
stay at that adapter boundary; metadata revisions share the session's private key.
